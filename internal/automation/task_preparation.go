package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
	// paidTaskOrderMissingOrderID 表示付款或砍价缺少订单号，禁止按会话猜单，等待同步后安全兜底。
	paidTaskOrderMissingOrderID paidTaskOrderResolution = "missing_order_id_no_chat_inference"
	// paidTaskOrderRepositoryUnavailable 表示自动化中心未配置订单仓储，因此未执行本地回填。
	paidTaskOrderRepositoryUnavailable paidTaskOrderResolution = "order_repository_unavailable"
	// paidTaskOrderNotFound 表示按账号和会话未找到可用的本地待发货订单。
	paidTaskOrderNotFound paidTaskOrderResolution = "local_pending_order_not_found"
	// paidTaskOrderResolved 表示已从本地待发货订单补齐订单号及商品事实。
	paidTaskOrderResolved paidTaskOrderResolution = "resolved_from_local_pending_order"
	// paidTaskOrderRecentResolved 表示已关联近期结束的订单，使简化重复事件能安全收口。
	paidTaskOrderRecentResolved paidTaskOrderResolution = "resolved_from_recent_order"
)

// simplifiedEventResolveWindow 限制简化交易事件按会话关联近期订单的回溯窗口。
const simplifiedEventResolveWindow = 30 * time.Minute

// resolvePaidTaskOrder 为交易系统消息补齐本账号已有订单事实，并保留简化付款事件的诊断结果。
// ctx 控制数据库查询取消，task 是原始事件；返回补齐后任务、可用于定位缺字段的结果码和查询错误。
func (c *Center) resolvePaidTaskOrder(ctx context.Context, task Task) (Task, paidTaskOrderResolution, error) {
	if task.TriggerType != TriggerOrderPaid && task.TriggerType != TriggerOrderCreated && task.TriggerType != TriggerBargainPending {
		return task, paidTaskOrderNotApplicable, nil
	}
	if c == nil || c.store == nil || c.store.Orders == nil {
		return task, paidTaskOrderRepositoryUnavailable, nil
	}
	if task.OrderID != "" {
		// order、err 保存本地订单事实及查询结果；缺失时由卖家身份门禁决定是否延期。
		order, err := c.store.Orders.Get(ctx, task.OrderID)
		if errors.Is(err, db.ErrNotFound) {
			return task, paidTaskOrderAlreadyKnown, nil
		}
		if err != nil {
			return task, paidTaskOrderAlreadyKnown, fmt.Errorf("按订单回填自动化事实: %w", err)
		}
		return mergeOrderIntoTask(task, order), paidTaskOrderAlreadyKnown, nil
	}
	if task.ChatID == "" {
		return task, paidTaskOrderMissingChat, nil
	}
	if task.TriggerType != TriggerOrderCreated {
		// 付款与砍价阶段事件缺少订单号时不按会话猜测订单：买家加商品也不能唯一确定一笔交易，
		// 同一买家再次购买同一商品而新订单尚未同步时，旧的待发货订单会被误发卡，已发货旧订单会把新消息误当重复消息收口。
		// 事件保持订单号缺失；付款事件由角色门禁安全收口，订单同步后的丢消息兜底由待发货扫描负责。
		return task, paidTaskOrderMissingOrderID, nil
	}
	if task.ItemID != "" {
		// 拍下事件已带商品，说明不是只含会话标识的简化事件，不改写。
		return task, paidTaskOrderNotApplicable, nil
	}
	// recent、err 保存仅拍下事件按会话命中的近期订单与查询错误；付款和砍价事件绝不进入此查询。
	recent, err := c.store.Orders.FindRecentByChat(ctx, task.AccountID, task.ChatID, task.BuyerID, task.ItemID, time.Now().Add(-simplifiedEventResolveWindow))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return task, paidTaskOrderNotFound, nil
		}
		return task, paidTaskOrderNotFound, fmt.Errorf("按会话识别近期订单: %w", err)
	}
	if recent == nil {
		return task, paidTaskOrderNotFound, nil
	}
	if c.logger != nil {
		c.logger.Info("简化交易事件已按会话关联近期订单", "account", task.AccountID, "order_id", recent.OrderID, "trigger", task.TriggerType)
	}
	return mergeOrderIntoTask(task, recent), paidTaskOrderRecentResolved, nil
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
	// needsDetail 首先保留货源跟价的显式按需查询，再判断付款事实缺失是否需要补全。
	needsDetail := task.RequireOrderDetail
	// existing、err 分别保存当前本地订单事实和查询错误；查询不可用时继续按平台详情路径补全。
	if existing, err := c.store.Orders.Get(ctx, task.OrderID); err == nil && existing != nil {
		task = mergeOrderIntoTask(task, existing)
	}
	needsDetail = needsDetail || task.TriggerType == TriggerOrderPaid && !paidOrderFactsComplete(task)
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
		return task, requireReliablePaidQuantity(task)
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
	return task, requireReliablePaidQuantity(task)
}

// paidOrderFactsComplete 判断付款发货所需数量、金额和完整规格是否齐全，避免恢复时重复平台查询。
func paidOrderFactsComplete(task Task) bool {
	return requireReliablePaidQuantity(task) == nil && strings.TrimSpace(task.Amount) != "" &&
		strings.TrimSpace(task.SpecName) != "" && strings.TrimSpace(task.SpecValue) != ""
}

// requireReliablePaidQuantity 要求付款发货在执行外部动作前已可靠取得正整数购买数量。
// 订单详情成功但缺少或无法识别 buyAmount 时，数量不得退化为 1 而少发卡密；返回错误使准备阶段延期重试。
func requireReliablePaidQuantity(task Task) error {
	if task.TriggerType != TriggerOrderPaid {
		return nil
	}
	if parsePositiveInt(task.Quantity) <= 0 {
		return fmt.Errorf("订单 %s 的购买数量缺失或无法识别，暂不自动发货", task.OrderID)
	}
	return nil
}
