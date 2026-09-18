package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// prepareTask 先合并本地订单事实，必要时再读取平台详情；任一持久化失败都会阻止外部动作。
func (c *Center) prepareTask(ctx context.Context, task Task) (Task, error) {
	// task、err 分别表示补全买家信息后的任务快照与准备阶段错误。
	task, err := c.prepareBuyerNickname(ctx, task)
	if err != nil {
		return task, err
	}
	if task.OrderID == "" {
		return task, nil
	}
	// upsertErr 保存自动化准备阶段订单事实写入结果；失败时禁止继续执行外部动作。
	if err := c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID: task.AccountID,
		ItemID:   task.ItemID,
		BuyerID:  task.BuyerID,
		ChatID:   task.ChatID,
	}); err != nil {
		return task, fmt.Errorf("保存自动化准备阶段订单事实: %w", err)
	}
	// needsDetail 表示付款发货或显式待付款货源跟价需要读取真实规格、数量和订单金额。
	needsDetail := task.TriggerType == TriggerOrderPaid || task.RequireOrderDetail
	// existing、err 分别保存当前本地订单事实和查询错误；查询不可用时继续按平台详情路径补全。
	if existing, err := c.store.Orders.Get(ctx, task.OrderID); err == nil && existing != nil {
		task = mergeOrderIntoTask(task, existing)
		if needsDetail && (existing.Quantity == "" || existing.Amount == "") {
			needsDetail = true
		}
		// 规则是否多规格由 action.config_json 决定；这里无法提前知道命中的 action，
		// 因此交易类事件统一补齐规格，确保后续规格映射有事实依据。
		if needsDetail && (existing.SpecName == "" || existing.SpecValue == "") {
			needsDetail = true
		}
	}
	// fetcher 是构造期固定的订单详情查询器；执行过程中不允许替换依赖。
	fetcher := c.dependencies.fetcher
	if !needsDetail || fetcher == nil {
		return task, nil
	}
	// cookieStr 仅在当前订单详情请求中使用明文登录凭证，不得写入日志或持久化快照。
	cookieStr := task.CookieStr
	if strings.TrimSpace(cookieStr) == "" {
		// cookieErr 表示从受控凭证边界读取当前账号 Cookie 的错误。
		var cookieErr error
		cookieStr, cookieErr = c.cookieValue(ctx, task.AccountID)
		if cookieErr != nil {
			return task, cookieErr
		}
	}
	// detail、detailErr 分别保存平台订单详情和查询错误。
	detail, detailErr := fetcher.FetchOrderDetail(ctx, task.AccountID, task.OrderID, task.ItemID, task.BuyerID, cookieStr)
	if detailErr != nil {
		return task, detailErr
	}
	if detail == nil {
		return task, nil
	}
	if detail.Quantity != "" {
		task.Quantity = detail.Quantity
	}
	if detail.SpecName != "" {
		task.SpecName = detail.SpecName
	}
	if detail.SpecValue != "" {
		task.SpecValue = detail.SpecValue
	}
	if detail.Amount != "" {
		task.Amount = detail.Amount
	}
	if detail.OrderStatus != "" {
		task.OrderStatus = detail.OrderStatus
	}
	if detail.ReceiverName != "" {
		task.ReceiverName = detail.ReceiverName
	}
	if detail.ReceiverPhone != "" {
		task.ReceiverPhone = detail.ReceiverPhone
	}
	if detail.ReceiverAddress != "" {
		task.ReceiverAddress = detail.ReceiverAddress
	}
	if detail.ReceiverCity != "" {
		task.ReceiverCity = detail.ReceiverCity
	}
	if len(detail.OrderFields) > 0 {
		task.OrderFields = detail.OrderFields
	}
	// upsertErr 保存补齐订单详情后的事实写入结果，失败时不允许进入动作执行阶段。
	if err := c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:      task.AccountID,
		ItemID:        task.ItemID,
		BuyerID:       task.BuyerID,
		ChatID:        task.ChatID,
		SpecName:      task.SpecName,
		SpecValue:     task.SpecValue,
		Quantity:      task.Quantity,
		Amount:        task.Amount,
		OrderStatus:   task.OrderStatus,
		ReceiverName:  task.ReceiverName,
		ReceiverPhone: task.ReceiverPhone,
		ReceiverAddr:  task.ReceiverAddress,
		ReceiverCity:  task.ReceiverCity,
	}); err != nil {
		return task, fmt.Errorf("保存订单详情事实: %w", err)
	}
	return task, nil
}
