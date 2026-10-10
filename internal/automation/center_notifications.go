package automation

import (
	"context"
	"xianyu-go/internal/db"
)

// notifyResult 根据规则执行结果发送通知。成功且实际发出了内容才通知，
// 避免无匹配动作的空跑刷屏；每次通知都以运行 ID 和终态持久化去重。
// notifyResult 封装notify结果业务协调。
func (c *Center) notifyResult(ctx context.Context, task Task, runID int64, status string, sent int, errMsg string) {
	c.notifications.notifyResult(ctx, task, runID, status, sent, errMsg)
}

// notifyRunNeedsReview 封装notify运行NeedsReview业务协调。
func (c *Center) notifyRunNeedsReview(ctx context.Context, run db.AutomationRun, reason string) {
	if c == nil {
		return
	}
	c.notifications.notifyRunNeedsReview(ctx, run, reason)
}
