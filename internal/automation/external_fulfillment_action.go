package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

// externalActionConfig 是自动发货动作中的外部货源最小配置。
type externalActionConfig struct {
	// SourceType 区分本地库存和外部货源。
	SourceType string `json:"source_type"`
	// InstanceID 是已配置货源实例主键。
	InstanceID int64 `json:"instance_id"`
	// GoodsID 是货源站商品主键。
	GoodsID int64 `json:"goods_id"`
	// GoodsName 是规则保存的货源商品名称，用于无规格商品的报价展示。
	GoodsName string `json:"goods_name"`
	// SpecName 是闲鱼商品规格名称，用于区分多规格报价分组。
	SpecName string `json:"spec_name"`
	// SpecValue 是闲鱼商品规格值，优先作为买家可见的报价标签。
	SpecValue string `json:"spec_value"`
	// SafePrice 是买家直接付款时继续使用的固定采购保护价。
	SafePrice string `json:"safe_price"`
	// Attach 保存直充商品字段与闲鱼订单数据的映射。
	Attach map[string]string `json:"attach"`
	// PendingPriceEnabled 表示买家拍下未付款时是否按实时货源价修改订单总价。
	PendingPriceEnabled bool `json:"pending_price_enabled"`
	// FixedMarkup 是每个实际采购单位增加的固定金额，使用十进制元字符串。
	FixedMarkup string `json:"fixed_markup"`
	// MinimumProfit 是付款采购时每个实际采购单位必须保留的最低利润。
	MinimumProfit string `json:"minimum_profit"`
}

// errExternalFulfillmentPending 表示货源站已经受理订单但尚未产出结果；协调器会把它交给独立长轮询策略，而不是普通三次失败重试。
var errExternalFulfillmentPending = errors.New("外部货源订单等待处理")

// externalFulfillmentActionError 标记外部货源尚未成功交付的动作错误，供运行收口在重试耗尽后发送买家提示。
type externalFulfillmentActionError struct {
	// err 保留原始错误分类；其中不得包含货源凭证或采购成本明细。
	err error
}

// Error 返回管理员可见的外部履约失败摘要。
func (e *externalFulfillmentActionError) Error() string { return e.err.Error() }

// Unwrap 保留安全重试和长轮询哨兵，避免买家通知改变原有恢复策略。
func (e *externalFulfillmentActionError) Unwrap() error { return e.err }

// externalFulfillmentFailed 把尚未成功采购的错误标记为外部履约失败；成功后发消息失败不使用该分类。
func externalFulfillmentFailed(err error) error {
	if err == nil {
		return nil
	}
	return &externalFulfillmentActionError{err: err}
}

// parseExternalActionConfig 解析外部货源配置，旧规则统一视为本地卡密。
func parseExternalActionConfig(raw string) (externalActionConfig, error) {
	// config 是动作配置的结构化结果。
	config := externalActionConfig{SourceType: "local", Attach: map[string]string{}}
	if strings.TrimSpace(raw) == "" {
		return config, nil
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil { // err 是外部货源动作配置的 JSON 解析错误。
		return externalActionConfig{}, errors.New("外部货源配置不是有效 JSON")
	}
	if strings.TrimSpace(config.SourceType) == "" {
		config.SourceType = "local"
	}
	if config.SourceType != "local" && config.SourceType != "external" {
		return externalActionConfig{}, errors.New("发货来源只支持 local 或 external")
	}
	if config.SourceType == "external" && (config.InstanceID <= 0 || config.GoodsID <= 0) {
		return externalActionConfig{}, errors.New("外部货源缺少实例或商品 ID")
	}
	return config, nil
}

// sendExternalFulfillment 使用闲鱼订单和动作 ID 幂等采购，再把已落库结果发给买家。
func (e *automationActionExecutor) sendExternalFulfillment(ctx context.Context, task Task, action db.AutomationAction, config externalActionConfig) (int, error) {
	if e.externalFulfillment == nil || e.externalFulfillment() == nil {
		return 0, externalFulfillmentFailed(fmt.Errorf("%w: 外部货源履约服务未初始化", errActionNotPerformed))
	}
	if strings.TrimSpace(task.OrderID) == "" {
		return 0, externalFulfillmentFailed(fmt.Errorf("%w: 外部货源采购缺少闲鱼订单号", errActionNotPerformed))
	}
	// userID 是当前闲鱼账号所属用户，用于隔离货源实例和采购单。
	userID, ownerErr := e.store.Cookies.GetOwnerID(ctx, task.AccountID)
	if ownerErr != nil {
		return 0, externalFulfillmentFailed(fmt.Errorf("%w: 读取账号归属: %v", errActionNotPerformed, ownerErr))
	}
	// count 是结合订单数量和每件份数后的实际采购数量。
	count := deliverySendCount(task, action)
	// externalOrderNo 在所有重试中保持不变，防止供应端重复扣款。
	externalOrderNo := fmt.Sprintf("xy-%s-a%d", strings.TrimSpace(task.OrderID), action.ID)
	// attach 是把规则中的闲鱼订单占位符替换为当前订单真实值后的直充参数。
	attach, attachErr := renderExternalAttach(config.Attach, task)
	if attachErr != nil {
		return 0, externalFulfillmentFailed(fmt.Errorf("%w: %v", errActionNotPerformed, attachErr))
	}
	// safePrice 默认使用规则固定保护价；只有该订单改价明确成功时才读取订单级动态保护价。
	safePrice := config.SafePrice
	// quotedSafePrice、quoted、quoteErr 分别是该动作订单级动态保护价、命中标记和读取失败原因。
	quotedSafePrice, quoted, quoteErr := e.store.Automation.AdjustedExternalSafePrice(ctx, task.OrderID, action.ID)
	if quoteErr != nil {
		return 0, externalFulfillmentFailed(fmt.Errorf("%w: 读取订单动态采购保护价: %v", errActionNotPerformed, quoteErr))
	}
	if quoted {
		safePrice = quotedSafePrice
	}
	// result、fulfillErr 是采购或使用原单号查询后的统一结果与错误。
	result, fulfillErr := e.externalFulfillment().Fulfill(ctx, ExternalFulfillmentRequest{UserID: userID, InstanceID: config.InstanceID, ExternalOrderNo: externalOrderNo, XianyuOrderID: task.OrderID, GoodsID: config.GoodsID, Quantity: count, SafePrice: safePrice, Attach: attach})
	if fulfillErr != nil {
		return 0, externalFulfillmentFailed(fmt.Errorf("%w: 外部货源履约失败: %v", errActionNotPerformed, fulfillErr))
	}
	if result.State == "unpaid" || result.State == "waiting" || result.State == "processing" {
		return 0, externalFulfillmentFailed(fmt.Errorf("%w: %w: 外部货源订单当前状态为 %s，稍后使用原单号查询", errActionNotPerformed, errExternalFulfillmentPending, result.State))
	}
	if result.State != "succeeded" {
		return 0, externalFulfillmentFailed(fmt.Errorf("%w: 外部货源订单当前状态为 %s，稍后使用原单号查询", errActionNotPerformed, result.State))
	}
	// messages 是需要顺序发送给买家的卡密或直充结果。
	messages := append([]string(nil), result.Cards...)
	if len(messages) == 0 && strings.TrimSpace(result.RechargeInfo) != "" {
		messages = append(messages, result.RechargeInfo)
	}
	if len(messages) == 0 && strings.TrimSpace(result.RechargeHints) != "" {
		messages = append(messages, result.RechargeHints)
	}
	if len(messages) == 0 {
		return 0, uncertainAction(errors.New("外部货源已成功但没有可发送的卡密或直充结果"))
	}
	// sent 是已明确成功发送的外部履约结果数量。
	sent := 0
	for _, message := range messages { // message 是当前待发送的一条卡密或直充结果。
		if sendErr := e.sendText(ctx, task, message); sendErr != nil {
			if errors.Is(sendErr, ErrMessageNotSent) {
				return sent, sendErr
			}
			return sent, uncertainAction(sendErr)
		}
		sent++
	}
	return sent, nil
}

// renderExternalAttach 把直充字段模板绑定到当前闲鱼订单，缺少动态值时在采购前安全停止。
func renderExternalAttach(configured map[string]string, task Task) (map[string]string, error) {
	// rendered 保存最终提交给货源站的直充字段。
	rendered := make(map[string]string, len(configured))
	for key, template := range configured { // key、template 是货源字段名和订单值模板。
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, errors.New("直充字段 key 不能为空")
		}
		// value 是替换闲鱼订单或已确认聊天占位符后的实际提交值。
		value := ""
		if strings.TrimSpace(template) == rechargeChatToken {
			value = strings.TrimSpace(task.OrderFields[rechargeChatField(key)])
		} else {
			value = strings.TrimSpace(renderTemplate(template, task))
		}
		if strings.Contains(value, "{") && strings.Contains(value, "}") {
			return nil, fmt.Errorf("直充字段 %s 包含不支持的订单占位符", key)
		}
		if strings.Contains(template, "{") && value == "" {
			return nil, fmt.Errorf("直充字段 %s 对应的闲鱼订单数据为空", key)
		}
		rendered[key] = value
	}
	return rendered, nil
}
