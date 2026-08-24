package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"xianyu-go/internal/db"
)

const (
	// rechargeChatToken 表示直充字段的值需要通过买家聊天收集。
	rechargeChatToken = "{chat_input}"
	// rechargeChatPrompt 是首次索取直充账号的固定业务文案。
	rechargeChatPrompt = "您好，本商品需要提供充值账号。请直接回复需要充值的手机号或平台账号，请勿发送其他内容。"
)

// RechargeChatMessage 是聊天层交给直充收集状态机的最小消息。
type RechargeChatMessage struct {
	AccountID string
	ChatID    string
	BuyerID   string
	Text      string
}

// prepareRechargeChatInput 在采购前收集或注入已确认的直充账号。
func (c *Center) prepareRechargeChatInput(ctx context.Context, task Task, runID int64, action db.AutomationAction) (Task, bool, error) {
	if action.ActionType != ActionSendCard {
		return task, false, nil
	}
	if !actionMatchesOrderSpec(task, action) {
		return task, false, nil
	}
	// config 是当前发货动作的外部货源配置。
	config, err := parseExternalActionConfig(action.ConfigJSON)
	if err != nil || config.SourceType != "external" {
		return task, false, err
	}
	// fieldKey 和 fieldName 标识唯一一个通过聊天收集的货源字段。
	fieldKey, fieldName := "", ""
	for key, value := range config.Attach { // key、value 是货源字段标识和已配置的数据来源。
		if strings.TrimSpace(value) != rechargeChatToken {
			continue
		}
		if fieldKey != "" {
			return task, false, errors.New("一条直充动作目前只支持一个聊天收集字段")
		}
		fieldKey = strings.TrimSpace(key)
	}
	if fieldKey == "" {
		return task, false, nil
	}
	if strings.TrimSpace(task.ChatID) == "" || strings.TrimSpace(task.BuyerID) == "" {
		return task, false, errors.New("聊天收集直充账号缺少 chat_id 或 buyer_id")
	}
	fieldName = fieldKey
	// taskKey 与延迟任务使用同一个幂等标识，确认后可精确唤醒。
	taskKey := task.AccountID + ":" + buildTriggerKey(task)
	if strings.TrimSpace(buildTriggerKey(task)) == "" {
		return task, false, errors.New("聊天收集直充账号缺少任务防重键")
	}
	// intake 是可跨重启恢复的收集记录。
	intake, err := c.store.EnsureRechargeChatIntake(ctx, db.RechargeChatIntake{
		TaskKey: taskKey, RunID: runID, CookieID: task.AccountID, ChatID: task.ChatID,
		BuyerID: normalizeRechargeBuyerID(task.BuyerID), OrderID: task.OrderID, ActionID: action.ID,
		FieldKey: fieldKey, FieldName: fieldName,
	})
	if err != nil {
		return task, false, fmt.Errorf("创建直充聊天收集任务: %w", err)
	}
	if intake.Status == "confirmed" {
		// value 只在真正调用货源接口的本次执行中解密。
		value, decryptErr := c.store.DecryptRechargeChatInput(intake.SecretOwner(), intake.ValueSecret)
		if decryptErr != nil {
			return task, false, fmt.Errorf("解密已确认的直充账号: %w", decryptErr)
		}
		if task.OrderFields == nil {
			task.OrderFields = map[string]string{}
		}
		task.OrderFields[rechargeChatField(fieldKey)] = value
		return task, false, nil
	}
	if !intake.PromptSent {
		if sendErr /* sendErr 是首次索取消息的传输错误。 */ := c.actions.sendText(ctx, task, rechargeChatPrompt); sendErr != nil {
			return task, false, sendErr
		}
		if markErr /* markErr 是索取消息发送标记的持久化错误。 */ := c.store.MarkRechargeChatPromptSent(ctx, intake.ID); markErr != nil {
			return task, false, fmt.Errorf("记录直充账号索取消息: %w", markErr)
		}
	}
	return task, true, nil
}

// HandleRechargeChat 消费直充账号、确认和重填消息；handled 为真时必须跳过 AI 和普通回复。
func (c *Center) HandleRechargeChat(ctx context.Context, message RechargeChatMessage) (handled bool, resultErr error) {
	if c == nil || c.store == nil {
		return false, nil
	}
	// intake 是与当前账号、会话和买家同时匹配的活跃收集任务。
	intake, err := c.store.FindActiveRechargeChatIntake(ctx, message.AccountID, message.ChatID, normalizeRechargeBuyerID(message.BuyerID))
	if errors.Is(err, db.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	// task 仅用来复用已有的在线消息发送边界。
	task := Task{AccountID: intake.CookieID, ChatID: intake.ChatID, BuyerID: intake.BuyerID, OrderID: intake.OrderID}
	text := strings.TrimSpace(message.Text) // text 是参与状态转换的买家文本。
	if intake.Status == "awaiting_input" {
		if text == "" {
			return true, nil
		}
		if text == "确认" || text == "重填" {
			return true, c.actions.sendText(ctx, task, rechargeChatPrompt)
		}
		if utf8.RuneCountInString(text) > 200 {
			return true, c.actions.sendText(ctx, task, "充值账号过长，请重新发送200个字以内的手机号或平台账号。")
		}
		// masked 仅是数据库备用摘要；买家确认消息按业务要求回显原值。
		masked := maskRechargeAccount(text)
		if err /* err 是加密保存候选账号时的持久化错误。 */ := c.store.SaveRechargeChatCandidate(ctx, *intake, text, masked); err != nil {
			return true, err
		}
		return true, c.actions.sendText(ctx, task, fmt.Sprintf("请确认充值账号：%s\n回复“确认”开始充值，回复“重填”重新输入。", text))
	}
	if text == "重填" {
		if err /* err 是重填时清除候选账号的持久化错误。 */ := c.store.ResetRechargeChatCandidate(ctx, intake.ID); err != nil {
			return true, err
		}
		return true, c.actions.sendText(ctx, task, rechargeChatPrompt)
	}
	if text != "确认" {
		// value、decryptErr 是已加密候选账号的短暂解密值和解密错误，原值只用于本次确认回显。
		value, decryptErr := c.store.DecryptRechargeChatInput(intake.SecretOwner(), intake.ValueSecret)
		if decryptErr != nil {
			return true, fmt.Errorf("解密待确认的直充账号: %w", decryptErr)
		}
		return true, c.actions.sendText(ctx, task, fmt.Sprintf("请确认充值账号：%s\n回复“确认”开始充值，回复“重填”重新输入。", value))
	}
	if err /* err 是确认状态和原任务唤醒的原子写入错误。 */ := c.store.ConfirmRechargeChatIntake(ctx, *intake); err != nil {
		return true, err
	}
	return true, c.actions.sendText(ctx, task, "已确认充值账号，正在提交充值。")
}

// rechargeChatField 生成仅在本次自动化执行中使用的聊天字段名。
func rechargeChatField(fieldKey string) string { return "chat_input:" + strings.TrimSpace(fieldKey) }

// normalizeRechargeBuyerID 去除闲鱼聊天用户标识的传输后缀。
func normalizeRechargeBuyerID(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), "@goofish")
}

// maskRechargeAccount 优先按手机号样式脱敏，其他平台账号保留首尾少量字符。
func maskRechargeAccount(value string) string {
	runes := []rune(strings.TrimSpace(value)) // runes 用于按 Unicode 字符数而非字节数脱敏。
	if len(runes) == 11 {
		return string(runes[:3]) + "****" + string(runes[7:])
	}
	if len(runes) <= 4 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:2]) + strings.Repeat("*", len(runes)-4) + string(runes[len(runes)-2:])
}
