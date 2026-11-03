package automation

import (
	"context"
	"fmt"
	"time"

	"xianyu-go/internal/db"
)

// logRunTerminal 按运行终态记录审计和结构化摘要；ctx携带关联标识，ruleID/runID为业务身份，status/sent/reason为已落库结果，started用于计算耗时。
func (r automationRunCoordinator) logRunTerminal(ctx context.Context, task Task, ruleID, runID int64, status string, sent int, reason string, started time.Time) {
	r.audit.runFinished(ctx, task, ruleID, runID, status, sent, reason)
	switch status {
	case "success":
		r.logger.InfoContext(ctx, "自动化规则执行成功", "status", status, "sent_count", sent, "duration", time.Since(started).Round(time.Millisecond))
	case "needs_review":
		r.logger.ErrorContext(ctx, "自动化规则转人工核对", "status", status, "sent_count", sent, "duration", time.Since(started).Round(time.Millisecond), "reason", reason)
	default:
		r.logger.WarnContext(ctx, "自动化规则执行失败", "status", status, "sent_count", sent, "duration", time.Since(started).Round(time.Millisecond), "reason", reason)
	}
}

// notifyExternalTerminalFailure 只在外部采购重试耗尽时通知买家；finalRun为最新落库状态，safePriceExceeded选择保护价提示；通知失败返回错误供运行统一记录。
func (r automationRunCoordinator) notifyExternalTerminalFailure(ctx context.Context, task Task, rule db.AutomationRule, finalRun *db.AutomationRun, safePriceExceeded bool) error {
	if r.notifyExternalFailure == nil || finalRun == nil || finalRun.NextRetryAt != 0 {
		return nil
	}
	// noticeErr 保存买家提示投递或幂等状态持久化失败，保留原运行日志和错误包装。
	if noticeErr := r.notifyExternalFailure(ctx, task, rule, safePriceExceeded); noticeErr != nil {
		r.logger.Error("发送外部采购最终失败提示失败", "run_id", finalRun.ID, "order_id", task.OrderID, "err", noticeErr)
		return fmt.Errorf("发送外部采购最终失败提示: %w", noticeErr)
	}
	return nil
}
