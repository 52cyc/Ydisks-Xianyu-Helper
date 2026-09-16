package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/mtop"
)

// adjustPriceTransientRetryLimit 是平台明确提示暂时无法改价时允许的最大请求次数，避免短暂订单状态同步延迟直接导致自动化失败。
const adjustPriceTransientRetryLimit = 5

// adjustPriceOrderCreatedInitialDelay 是收到拍下未付款事件后首次请求改价前的等待时间，给闲鱼订单状态预留同步窗口。
var adjustPriceOrderCreatedInitialDelay = 2 * time.Second

// adjustPriceTransientRetryGap 是相邻两次暂时性改价失败之间的等待时间；测试可临时缩短它以验证重试分支。
var adjustPriceTransientRetryGap = 3 * time.Second

// errAdjustPriceNaturallyClosed 表示买家已付款或订单状态已结束，当前改价流程应无错误收口且不得再次请求。
var errAdjustPriceNaturallyClosed = errors.New("订单状态已结束，改价流程自然结束")

// adjustOrderPriceWithRetry 为规则改价和 AI 报价共用平台暂忙重试；传输结果未知和明确业务拒绝均不得自动重放。
func (e *automationActionExecutor) adjustOrderPriceWithRetry(ctx context.Context, task Task, priceCents int64) error {
	if task.TriggerType == TriggerOrderCreated && adjustPriceOrderCreatedInitialDelay > 0 {
		// initialTimer 延迟首次改价，避免闲鱼刚建单但尚未开放改价能力时产生一次无效请求。
		initialTimer := time.NewTimer(adjustPriceOrderCreatedInitialDelay)
		select {
		case <-ctx.Done():
			if !initialTimer.Stop() {
				select {
				case <-initialTimer.C:
				default:
				}
			}
			return fmt.Errorf("%w: 订单首次改价等待被取消: %v", errActionNotPerformed, ctx.Err())
		case <-initialTimer.C:
		}
	}
	// attempt 是当前已发送的改价请求次数，最多执行预设次数以避免无限占用自动化工作线程。
	for attempt := 1; attempt <= adjustPriceTransientRetryLimit; attempt++ {
		// attemptErr 是本次请求、凭证恢复和 Cookie 条件写回后的最终结果。
		attemptErr := e.adjustOrderPriceAttempt(ctx, task, priceCents, true)
		if attemptErr == nil {
			return nil
		}
		if mtop.IsAdjustPriceNaturallyClosed(attemptErr) {
			return fmt.Errorf("%w: %v", errAdjustPriceNaturallyClosed, attemptErr)
		}
		// uncertain 表示请求可能已被平台执行，重复提交会造成价格状态无法判定，必须交由人工核对。
		var uncertain *uncertainActionError
		if errors.As(attemptErr, &uncertain) || !isAdjustPriceTransientBusy(attemptErr) || attempt == adjustPriceTransientRetryLimit {
			return attemptErr
		}
		e.logger.Info("订单改价暂时不可用，稍后重试", "account", task.AccountID, "order_id", task.OrderID, "attempt", attempt, "limit", adjustPriceTransientRetryLimit)
		// retryTimer 控制下一次平台请求的最小间隔，并在上下文取消时立即释放本次自动化任务。
		retryTimer := time.NewTimer(adjustPriceTransientRetryGap)
		select {
		case <-ctx.Done():
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			return fmt.Errorf("%w: 订单改价重试等待被取消: %v", errActionNotPerformed, ctx.Err())
		case <-retryTimer.C:
		}
	}
	return fmt.Errorf("%w: 订单改价重试次数耗尽", errActionNotPerformed)
}

// isAdjustPriceTransientBusy 判断平台是否明确返回订单状态尚未同步完成的暂时性改价失败。
func isAdjustPriceTransientBusy(err error) bool {
	if err == nil {
		return false
	}
	// message 是不含凭证明文的远端业务错误文本，只匹配已验证的暂时性返回而不重试订单关闭等终态错误。
	message := err.Error()
	return strings.Contains(message, "CANNOT_MODIFY_FEE") || strings.Contains(message, "稍后重试") || strings.Contains(message, "稍后再试")
}
