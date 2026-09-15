package automation

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/db"
)

// paidTaskOrderResolution 描述简化付款事件从本地订单回填关键事实的结果，仅用于无敏感数据的诊断日志。
type paidTaskOrderResolution string

const (
	// paidTaskOrderNotApplicable 表示当前事件不是需要本地回填的简化付款事件。
	paidTaskOrderNotApplicable paidTaskOrderResolution = "not_applicable"
	// paidTaskOrderAlreadyKnown 表示付款事件已携带订单号，无需按会话查找本地订单。
	paidTaskOrderAlreadyKnown paidTaskOrderResolution = "order_id_already_present"
	// paidTaskOrderMissingChat 表示付款事件同时缺少订单号和会话标识，无法执行安全回填。
	paidTaskOrderMissingChat paidTaskOrderResolution = "missing_chat_id"
	// paidTaskOrderRepositoryUnavailable 表示自动化中心未配置订单仓储，因此未执行本地回填。
	paidTaskOrderRepositoryUnavailable paidTaskOrderResolution = "order_repository_unavailable"
	// paidTaskOrderNotFound 表示按账号和会话未找到可用的本地待发货订单。
	paidTaskOrderNotFound paidTaskOrderResolution = "local_pending_order_not_found"
	// paidTaskOrderResolved 表示已从本地待发货订单补齐订单号及商品事实。
	paidTaskOrderResolved paidTaskOrderResolution = "resolved_from_local_pending_order"
)

// resolvePaidTaskOrder 为只有会话标识的简化付款消息回填本账号最近待发货订单。
// ctx 控制数据库查询取消，task 是原始事件；返回补齐后任务、可用于定位缺字段的结果码和查询错误。
func (c *Center) resolvePaidTaskOrder(ctx context.Context, task Task) (Task, paidTaskOrderResolution, error) {
	if task.TriggerType != TriggerOrderPaid {
		return task, paidTaskOrderNotApplicable, nil
	}
	if task.OrderID != "" {
		return task, paidTaskOrderAlreadyKnown, nil
	}
	if task.ChatID == "" {
		return task, paidTaskOrderMissingChat, nil
	}
	if c == nil || c.store == nil || c.store.Orders == nil {
		return task, paidTaskOrderRepositoryUnavailable, nil
	}
	// order 保存按账号、会话以及可选买家和商品条件命中的待发货订单。
	order, err := c.store.Orders.FindLatestPendingByChat(ctx, task.AccountID, task.ChatID, task.BuyerID, task.ItemID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return task, paidTaskOrderNotFound, nil
		}
		return task, paidTaskOrderNotFound, fmt.Errorf("按会话回填自动发货订单: %w", err)
	}
	if order == nil {
		return task, paidTaskOrderNotFound, nil
	}
	return mergeOrderIntoTask(task, order), paidTaskOrderResolved, nil
}

// prepareBuyerNickname 补齐模板渲染所需的买家昵称摘要。
func (c *Center) prepareBuyerNickname(ctx context.Context, task Task) (Task, error) {
	if task.BuyerNickname != "" || task.ChatID == "" {
		return task, nil
	}
	// nickname 保存聊天会话中可用于模板渲染的买家昵称。
	nickname, nicknameErr := c.store.Chats.BuyerNicknameForAutomation(ctx, task.AccountID, task.ChatID)
	if nicknameErr != nil {
		return task, fmt.Errorf("读取买家昵称: %w", nicknameErr)
	}
	task.BuyerNickname = nickname
	return task, nil
}

// mergeOrderIntoTask 用本地订单事实补全自动化任务中尚未获得的字段。
func mergeOrderIntoTask(task Task, order *db.Order) Task {
	if task.OrderID == "" {
		task.OrderID = order.OrderID
	}
	if task.ItemID == "" {
		task.ItemID = order.ItemID
	}
	if task.BuyerID == "" {
		task.BuyerID = order.BuyerID
	}
	if task.ChatID == "" {
		task.ChatID = order.ChatID
	}
	if task.SpecName == "" {
		task.SpecName = order.SpecName
	}
	if task.SpecValue == "" {
		task.SpecValue = order.SpecValue
	}
	if task.Quantity == "" {
		task.Quantity = order.Quantity
	}
	if task.Amount == "" {
		task.Amount = order.Amount
	}
	if task.OrderStatus == "" {
		task.OrderStatus = order.OrderStatus
	}
	if task.ReceiverName == "" {
		task.ReceiverName = order.ReceiverName
	}
	if task.ReceiverPhone == "" {
		task.ReceiverPhone = order.ReceiverPhone
	}
	if task.ReceiverAddress == "" {
		task.ReceiverAddress = order.ReceiverAddr
	}
	if task.ReceiverCity == "" {
		task.ReceiverCity = order.ReceiverCity
	}
	// 本地订单同步只会在平台明确识别砍价活动时写入真值；任务一旦携带真值不得在后续补全中降级。
	if order.IsBargain != 0 {
		task.IsBargain = true
	}
	return task
}
