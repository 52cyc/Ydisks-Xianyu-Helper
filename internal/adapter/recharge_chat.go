package adapter

import (
	"context"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/engine"
)

// HandleChatWorkflow 把买家文本优先交给直充收集状态机，命中后不再进入 AI 或普通回复。
func (a *Adapter) HandleChatWorkflow(ctx context.Context, message engine.ChatMessage) (bool, error) {
	if a == nil || a.automation == nil {
		return false, nil
	}
	return a.automation.HandleRechargeChat(ctx, automation.RechargeChatMessage{
		AccountID: message.AccountID,
		ChatID:    message.ChatID,
		BuyerID:   message.SenderUserID,
		Text:      message.Text,
	})
}
