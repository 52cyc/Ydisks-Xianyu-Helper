package adapter

import (
	"context"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/engine"
)

// HandleChatWorkflow 依次处理直充账号收集和外部跟价咨询引导，命中后不再进入 AI 或普通回复。
func (a *Adapter) HandleChatWorkflow(ctx context.Context, message engine.ChatMessage) (bool, error) {
	if a == nil || a.automation == nil {
		return false, nil
	}
	// workflowMessage 是自动化聊天状态机使用的非敏感会话事实。
	workflowMessage := automation.RechargeChatMessage{
		AccountID: message.AccountID,
		ChatID:    message.ChatID,
		BuyerID:   message.SenderUserID,
		Text:      message.Text,
	}
	// rechargeHandled 和 rechargeErr 表示当前消息是否属于已经进行中的直充账号确认流程。
	rechargeHandled, rechargeErr := a.automation.HandleRechargeChat(ctx, workflowMessage)
	if rechargeHandled || rechargeErr != nil {
		return rechargeHandled, rechargeErr
	}
	return a.automation.HandleExternalPriceGuidanceChat(ctx, workflowMessage, message.ItemID)
}
