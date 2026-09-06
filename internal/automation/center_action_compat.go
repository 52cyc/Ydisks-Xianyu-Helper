package automation

import (
	"context"

	"xianyu-go/internal/db"
)

// executeAction 将具体动作委托给发货动作执行器。
func (c *Center) executeAction(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	return c.actions.executeAction(ctx, task, action)
}

// executeActionWithProof 执行动作并把当前运行内已成功投递的凭证传给确认发货动作。
func (c *Center) executeActionWithProof(ctx context.Context, task Task, action db.AutomationAction, proof shipmentDeliveryProof) (actionExecutionResult, error) {
	return c.actions.executeActionWithProof(ctx, task, action, proof)
}

// confirmShipment 将确认发货委托给发货动作执行器。
func (c *Center) confirmShipment(ctx context.Context, task Task) error {
	return c.actions.confirmShipment(ctx, task)
}

// wakeCredentialBlockedAutomation 在 Cookie 更新后唤醒凭证阻塞的自动化任务。
func (c *Center) wakeCredentialBlockedAutomation(ctx context.Context, accountID string) {
	if c == nil || c.store == nil || c.store.Automation == nil {
		return
	}
	if // err 用于本次流程后续判断的err
	err := c.store.Automation.WakeCredentialBlocked(ctx, accountID); err != nil {
		c.logger.Warn("Cookie 更新后唤醒自动化任务失败", "account", accountID, "err", err)
	}
}

// sendCard 将卡密发送委托给发货动作执行器。
func (c *Center) sendCard(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	return c.actions.sendCard(ctx, task, action)
}

// accountAutomationAllowed 判断账号是否仍允许执行自动化动作。
func (c *Center) accountAutomationAllowed(ctx context.Context, accountID string) (bool, error) {
	return c.taskRunner.accountAutomationAllowed(ctx, accountID)
}

// accountSenderReady 判断账号是否具备可发送自动化消息的在线连接。
func (c *Center) accountSenderReady(accountID string) bool {
	if c == nil || c.senders == nil {
		return false
	}
	// sender、ok 用于本次流程后续判断的sender、ok
	sender, ok := c.senders.Sender(accountID)
	if !ok {
		return false
	}
	if // ready、ok 用于本次流程后续判断的ready、ok
	ready, ok := sender.(automationReadySender); ok {
		return ready.AutomationReady()
	}
	return true
}

// cardContent 获取卡密组内容的兼容入口。
func (c *Center) cardContent(ctx context.Context, card *db.CardFull) (text, imageURL string, err error) {
	return c.actions.cardContent(ctx, card)
}

// sendImage 将图片消息发送委托给发货动作执行器。
func (c *Center) sendImage(ctx context.Context, task Task, imageURL string, cardID int64) error {
	return c.actions.sendImage(ctx, task, imageURL, cardID)
}

// cookieValue 读取账号 Cookie 的兼容入口。
func (c *Center) cookieValue(ctx context.Context, cookieID string) (string, error) {
	return c.actions.cookieValue(ctx, cookieID)
}
