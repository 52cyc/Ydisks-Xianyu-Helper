package automation

import (
	"context"
	"fmt"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestExternalPendingPriceNaturallyEndsWhenPaidFlowTakesOver 验证付款流程并发接管报价后，待付款改价不再报“报价已经收口”。
func TestExternalPendingPriceNaturallyEndsWhenPaidFlowTakesOver(t *testing.T) {
	useFastAdjustPriceInitialDelay(t)
	// store、cleanup 提供隔离数据库和释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是报价创建、并发替换和状态检查共用的上下文。
	ctx := context.Background()
	// adjustStarted、adjustRelease 分别通知改价已经开始和控制平台响应返回时机。
	adjustStarted, adjustRelease := make(chan struct{}), make(chan struct{})
	// platform 在付款流程替换报价后返回订单状态已经不支持改价。
	platform := &fakeMTop{adjustRet: []string{"FAIL_BIZ_BAD_REQUEST::当前订单状态不支持改价"}, adjustStarted: adjustStarted, adjustRelease: adjustRelease}
	// center 是执行待付款动态跟价的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: platform})
	// action 是本次报价对应的外部履约动作，非零 ID 满足报价持久化约束。
	action := db.AutomationAction{ID: 91, ActionType: ActionSendCard, Enabled: true}
	// actionQuote 是待付款流程准备持久化的实时成本和动态保护价快照。
	actionQuote := pendingPriceActionQuote{action: action, config: externalActionConfig{SourceType: "external"}, unitCostCents: 280,
		fulfillmentQuantity: 1, fixedMarkupCents: 50, minimumProfitCents: 20, unitTargetCents: 330}
	// task 是即将被付款事件抢先推进状态的订单事实。
	task := Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "paid-takes-over", ItemID: "item"}
	// resultErr 异步接收待付款改价的最终收口结果。
	resultErr := make(chan error, 1)
	// 改价调用阻塞期间模拟真实 order_paid 流程把 pending 报价替换为 adjusted 快照。
	go func() {
		resultErr <- center.persistAndApplyPendingPrice(ctx, task, db.AutomationRule{}, []pendingPriceActionQuote{actionQuote}, 330)
	}()
	select {
	case <-adjustStarted:
	case <-time.After(time.Second):
		t.Fatal("adjust price call did not start")
	}
	// paidQuote 是付款流程基于实付和实时成本写入的最终采购保护价快照。
	paidQuote := db.ExternalPriceQuote{OrderID: task.OrderID, CookieID: task.AccountID, ActionID: action.ID, UnitCostCents: 280,
		FulfillmentQuantity: 1, FixedMarkupCents: 50, TargetOrderCents: 330, DynamicSafePrice: "3.30", Status: "adjusted"}
	if replaceErr := store.Automation.ReplaceExternalPriceQuotesAsAdjusted(ctx, []db.ExternalPriceQuote{paidQuote}); replaceErr != nil { // replaceErr 是付款流程接管报价失败的原因。
		t.Fatal(replaceErr)
	}
	close(adjustRelease)
	// naturalErr 是付款流程接管后待付款改价的最终返回，必须为空。
	var naturalErr error
	select {
	case naturalErr = <-resultErr:
		if naturalErr != nil {
			t.Fatalf("paid takeover should naturally close repricing: %v", naturalErr)
		}
	case <-time.After(time.Second):
		t.Fatal("repricing did not finish after platform response")
	}
	// exists、status、stateErr 验证付款流程写入的 adjusted 快照未被自然结束分支覆盖。
	exists, status, stateErr := store.Automation.ExternalPriceQuoteState(ctx, task.OrderID)
	if stateErr != nil || !exists || status != "adjusted" {
		t.Fatalf("exists=%v status=%q err=%v", exists, status, stateErr)
	}
}

// TestParseExternalPriceMessageConfigUpgradesLegacyDefault 验证旧版默认引导会自动迁移为包含实时价格列表的新流程。
func TestParseExternalPriceMessageConfigUpgradesLegacyDefault(t *testing.T) {
	// raw 是升级前已经保存在规则中的默认咨询话术。
	raw := `{"price_guidance_enabled":true,"price_guidance_text":"` + legacyExternalPriceGuidance + `"}`
	// config、configErr 是兼容解析后的查询提示和报价话术。
	config, configErr := parseExternalPriceMessageConfig(raw)
	if configErr != nil {
		t.Fatal(configErr)
	}
	if config.QueryPromptText != defaultExternalPriceQueryPrompt || config.GuidanceText != defaultExternalPriceGuidance {
		t.Fatalf("旧版默认话术未升级: %+v", config)
	}
	if config.SuccessNoticeText != defaultExternalFulfillmentSuccessNotice {
		t.Fatalf("未配置成功文案时应使用默认第二条消息: %q", config.SuccessNoticeText)
	}
	// failureRaw 是升级前保存的通用采购失败默认提示。
	failureRaw := `{"fulfillment_failure_notice_enabled":true,"fulfillment_failure_notice_text":"` + legacyExternalFulfillmentFailureNotice + `"}`
	// failureConfig、failureErr 是升级后的保护价和货源站异常两类默认提示。
	failureConfig, failureErr := parseExternalPriceMessageConfig(failureRaw)
	if failureErr != nil || failureConfig.FailureNoticeText != defaultExternalFulfillmentFailureNotice || failureConfig.SafePriceFailureNoticeText != defaultExternalSafePriceFailureNotice {
		t.Fatalf("旧版采购失败话术未升级: config=%+v err=%v", failureConfig, failureErr)
	}
	if _, invalidErr := parseExternalPriceMessageConfig(`{"fulfillment_success_notice_text":"缺少卡密占位符"}`); invalidErr == nil {
		t.Fatal("缺少 delivery_content 的成功文案必须拒绝执行")
	}
}

// TestExternalPendingPriceAdjustsAndPersistsDynamicSafePrice 验证实时单价、可配置固定加价和最低利润生成订单级改价及采购保护价。
func TestExternalPendingPriceAdjustsAndPersistsDynamicSafePrice(t *testing.T) {
	useFastAdjustPriceInitialDelay(t)
	// store、cleanup 保存隔离数据库和测试结束后的连接清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是规则、报价和待付款事件共用的测试上下文。
	ctx := context.Background()
	// owner 是测试账号所属用户，用于创建付款后外部货源规则。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// ruleID 是付款规则主键；每个购买单位按实时成本增加 0.50 元，并至少保留 0.20 元利润。
	ruleID, createErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: owner.ID, CookieID: "cid", ItemID: "item-price",
		Name: "外部跟价", TriggerType: TriggerOrderPaid, Enabled: true,
		ConfigJSON: `{"price_guidance_enabled":true,"price_query_prompt_text":"正在查询 {item_id}","price_guidance_text":"当前报价：{price_list}，需要请拍下不要付款","price_adjusted_notice_enabled":true,"price_adjusted_notice_text":"最新总价 ¥{price}，数量 {quantity}"}`,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendCard,
			DeliveryCount: 1, Enabled: true, ConfigJSON: `{"source_type":"external","instance_id":8,"goods_id":40863,"safe_price":"2.80","pending_price_enabled":true,"fixed_markup":"0.50","minimum_profit":"0.20"}`}}})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// rule 是重新读取后带持久化动作 ID 的规则快照。
	rule, ruleErr := store.Automation.Get(ctx, ruleID)
	if ruleErr != nil || rule == nil || len(rule.Actions) != 1 {
		t.Fatalf("读取测试规则失败: rule=%+v err=%v", rule, ruleErr)
	}
	// fulfillment 返回每件 2.80 元且可采购的实时货源商品。
	fulfillment := &externalFulfillmentStub{product: ExternalProductQuote{Price: "2.80", CanBuy: true}}
	// platform 明确确认闲鱼订单改价成功。
	platform := &fakeMTop{adjustOk: true, adjustRet: []string{"SUCCESS::调用成功"}}
	// sender 接收查询提示、实时咨询报价和改价成功后的最终价格通知。
	sender := &testSender{}
	// center 注入订单详情、实时货源和平台改价能力；购买数量为二。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{MTop: platform,
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "2", Amount: "20.00"}}, ExternalFulfillment: fulfillment})
	// chatMessage 是买家首次咨询该商品时的会话事实。
	chatMessage := RechargeChatMessage{AccountID: "cid", ChatID: "chat", BuyerID: "buyer", Text: "怎么购买"}
	if handled, guideErr := center.HandleExternalPriceGuidanceChat(ctx, chatMessage, "item-price"); guideErr != nil || !handled { // handled 和 guideErr 是首次引导的短路状态和发送错误。
		t.Fatalf("首次咨询引导失败: handled=%v err=%v", handled, guideErr)
	}
	if handled, guideErr := center.HandleExternalPriceGuidanceChat(ctx, chatMessage, "item-price"); guideErr != nil || handled { // handled 和 guideErr 验证同会话不重复发送引导且允许后续普通回复。
		t.Fatalf("重复咨询不应再次发送引导: handled=%v err=%v", handled, guideErr)
	}
	if len(sender.texts) != 2 || sender.texts[0] != "正在查询 item-price" || sender.texts[1] != "当前报价：¥3.30，需要请拍下不要付款" {
		t.Fatalf("咨询实时报价或变量替换异常: %#v", sender.texts)
	}
	// guidanceDedupeKey 是本规则和聊天会话的稳定报价防重键。
	guidanceDedupeKey := fmt.Sprintf("external-price-guide:%d:%s", ruleID, chatMessage.ChatID)
	if _, expireErr := store.DB.ExecContext(ctx, `UPDATE external_price_message_records SET lease_expires_at=1 WHERE dedupe_key=?`, guidanceDedupeKey); expireErr != nil { // expireErr 是测试夹具把报价有效期推进到过去的写入错误。
		t.Fatal(expireErr)
	}
	if handled, guideErr := center.HandleExternalPriceGuidanceChat(ctx, chatMessage, "item-price"); guideErr != nil || !handled { // handled 和 guideErr 验证过期后的下一条咨询会重新查价并发送。
		t.Fatalf("过期后重新报价失败: handled=%v err=%v", handled, guideErr)
	}
	if len(sender.texts) != 4 || sender.texts[2] != "正在查询 item-price" || sender.texts[3] != "当前报价：¥3.30，需要请拍下不要付款" {
		t.Fatalf("过期后应重新发送完整报价: %#v", sender.texts)
	}
	// task 是买家已拍下但尚未付款的真实订单事件。
	task := Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "order-price", ItemID: "item-price", BuyerID: "buyer", ChatID: "chat"}
	if handleErr := center.HandleTask(ctx, task); handleErr != nil { // handleErr 是首次待付款跟价处理结果。
		t.Fatal(handleErr)
	}
	if platform.adjustCalls != 1 || platform.adjustCentsIn != 660 {
		t.Fatalf("目标价格应为 (2.80+0.50)*2=6.60: calls=%d cents=%d", platform.adjustCalls, platform.adjustCentsIn)
	}
	if len(sender.texts) != 5 || sender.texts[4] != "最新总价 ¥6.60，数量 2" {
		t.Fatalf("改价成功通知应携带最终总价和数量: %#v", sender.texts)
	}
	// dynamicSafePrice、quoted、safeErr 是动作在改价成功后可用于付款采购的订单级保护价。
	dynamicSafePrice, quoted, safeErr := store.Automation.AdjustedExternalSafePrice(ctx, "order-price", rule.Actions[0].ID)
	if safeErr != nil || !quoted || dynamicSafePrice != "6.20" {
		t.Fatalf("动态保护价应为 (2.80+0.50-0.20)*2=6.20: price=%q quoted=%v err=%v", dynamicSafePrice, quoted, safeErr)
	}
	// duplicateErr 是重复待付款卡片处理结果；已有报价时不得再次修改价格。
	if duplicateErr := center.HandleTask(ctx, task); duplicateErr != nil {
		t.Fatal(duplicateErr)
	}
	if platform.adjustCalls != 1 {
		t.Fatalf("重复待付款事件不应再次改价: calls=%d", platform.adjustCalls)
	}
	if len(sender.texts) != 5 {
		t.Fatalf("重复待付款事件不应再次发送改价通知: %#v", sender.texts)
	}
}

// TestProfitRatePricingRoundsUpToCent 验证实时采购价按利润率计算时向上取整到分。
func TestProfitRatePricingRoundsUpToCent(t *testing.T) {
	// config 是两个百分点的外部货源利润率配置。
	config := externalActionConfig{ProfitRate: "2.00"}
	// target、markup、minimum 和 err 是 9.50 元采购价的目标价、加价、兼容最低利润及计算错误，单位为分。
	target, markup, minimum, err := externalUnitTargetCents(config, 950)
	if err != nil || target != 969 || markup != 19 || minimum != 0 {
		t.Fatalf("9.50 按 2%% 利润率应为 9.69: target=%d markup=%d minimum=%d err=%v", target, markup, minimum, err)
	}
	target, _, _, err = externalUnitTargetCents(config, 1343)
	if err != nil || target != 1370 {
		t.Fatalf("13.43 按 2%% 利润率应向上取整为 13.70: target=%d err=%v", target, err)
	}
}

// TestExternalListingTargetUsesProfitRate 验证商品页同步价复用外部货源利润率计算。
func TestExternalListingTargetUsesProfitRate(t *testing.T) {
	// center 只注入返回 9.50 元实时价的外部货源报价能力。
	center := NewWithDependencies(nil, nil, nil, CenterDependencies{ExternalFulfillment: &externalFulfillmentStub{
		product: ExternalProductQuote{Price: "9.50", CanBuy: true},
	}})
	// rule 是开启两个百分点利润率同步的单规格商品规则。
	rule := db.AutomationRule{UserID: 7, Actions: []db.AutomationAction{{ActionType: ActionSendCard, DeliveryCount: 1, Enabled: true,
		ConfigJSON: `{"source_type":"external","instance_id":8,"goods_id":4994,"price_sync_enabled":true,"profit_rate":"2.00"}`}}}
	// target、enabled 和 err 是商品页目标价分值、同步开关命中状态及报价错误。
	target, enabled, err := center.externalListingTargetCents(context.Background(), rule)
	if err != nil || !enabled || target != 969 {
		t.Fatalf("商品页同步价应为 9.69: target=%d enabled=%v err=%v", target, enabled, err)
	}
}

// TestQuoteExternalPriceGuidanceBuildsMultiSpecList 验证咨询报价按规格聚合同规格多条发货内容，并保持规则顺序。
func TestQuoteExternalPriceGuidanceBuildsMultiSpecList(t *testing.T) {
	// store、cleanup 保存包含管理员账号的隔离数据库及释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是账号归属和实时报价共用的测试上下文。
	ctx := context.Background()
	// owner 是测试闲鱼账号的所属用户。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	if createErr := store.Cookies.CreateOwned(ctx, "multi-account", "cookie", owner.ID); createErr != nil { // createErr 是测试账号写入错误。
		t.Fatal(createErr)
	}
	// fulfillment 为三个货源商品返回不同的当前采购价。
	fulfillment := &externalFulfillmentStub{products: map[int64]ExternalProductQuote{
		101: {Price: "2.00", CanBuy: true},
		102: {Price: "1.00", CanBuy: true},
		103: {Price: "5.00", CanBuy: true},
	}}
	// center 只注入咨询报价所需的外部货源能力。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{ExternalFulfillment: fulfillment})
	// rule 包含 30 天规格的两条发货内容和 90 天规格的一条发货内容。
	rule := db.AutomationRule{Actions: []db.AutomationAction{
		{ActionType: ActionSendCard, DeliveryCount: 1, Enabled: true, ConfigJSON: `{"source_type":"external","instance_id":1,"goods_id":101,"spec_name":"周期","spec_value":"30天","pending_price_enabled":true,"fixed_markup":"0.50"}`},
		{ActionType: ActionSendCard, DeliveryCount: 2, Enabled: true, ConfigJSON: `{"source_type":"external","instance_id":1,"goods_id":102,"spec_name":"周期","spec_value":"30天","pending_price_enabled":true,"fixed_markup":"0.20"}`},
		{ActionType: ActionSendCard, DeliveryCount: 1, Enabled: true, ConfigJSON: `{"source_type":"external","instance_id":1,"goods_id":103,"spec_name":"周期","spec_value":"90天","pending_price_enabled":true,"fixed_markup":"0.80"}`},
	}}
	// price、priceList、quoteErr 是多规格报价结果；price 应为空以避免把某一规格误作统一单价。
	price, priceList, quoteErr := center.quoteExternalPriceGuidance(ctx, "multi-account", rule)
	if quoteErr != nil {
		t.Fatal(quoteErr)
	}
	if price != "" || priceList != "30天：¥4.90\n90天：¥5.80" {
		t.Fatalf("多规格报价列表异常: price=%q list=%q", price, priceList)
	}
}

// TestExternalFulfillmentUsesDynamicSafePriceOnlyAfterSuccessfulAdjustment 验证付款采购只在订单改价成功后覆盖固定保护价。
func TestExternalFulfillmentUsesDynamicSafePriceOnlyAfterSuccessfulAdjustment(t *testing.T) {
	// store、cleanup 保存隔离数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是报价持久化和采购动作共用的测试上下文。
	ctx := context.Background()
	// action 是本次采购使用的稳定动作标识和固定保护价配置。
	action := db.AutomationAction{ID: 99, ActionType: ActionSendCard, DeliveryCount: 1,
		ConfigJSON: `{"source_type":"external","instance_id":8,"goods_id":40863,"safe_price":"2.80","pending_price_enabled":true,"fixed_markup":"0.50","minimum_profit":"0.20"}`}
	// quote 是尚未改价成功的订单级动态保护价快照。
	quote := db.ExternalPriceQuote{OrderID: "order-paid", CookieID: "cid", ActionID: action.ID, UnitCostCents: 280,
		FulfillmentQuantity: 1, FixedMarkupCents: 50, MinimumProfitCents: 20, TargetOrderCents: 330, DynamicSafePrice: "3.10"}
	if created, createErr := store.Automation.CreateExternalPriceQuotes(ctx, []db.ExternalPriceQuote{quote}); createErr != nil || !created { // created、createErr 是报价唯一创建权和落库结果。
		t.Fatalf("创建报价失败: created=%v err=%v", created, createErr)
	}
	// sender 接收采购成功后的测试卡密；fulfillment 记录实际传入的保护价。
	sender := &testSender{}
	fulfillment := &externalFulfillmentStub{result: ExternalFulfillmentResult{State: "succeeded", Cards: []string{"CARD"}}} // fulfillment 记录两次采购分别使用的保护价。
	// executor 是只注入采购与消息发送能力的动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}, externalFulfillment: func() ExternalFulfillment { return fulfillment }}
	// task 是买家已经付款的订单事实。
	task := Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "order-paid", ItemID: "item", BuyerID: "buyer", ChatID: "chat", Quantity: "1"}
	// config 是从动作 JSON 解析出的货源配置。
	config, configErr := parseExternalActionConfig(action.ConfigJSON)
	if configErr != nil {
		t.Fatal(configErr)
	}
	if _, sendErr := executor.sendExternalFulfillment(ctx, task, action, config); sendErr != nil { // sendErr 是 pending 报价下的采购发送结果。
		t.Fatal(sendErr)
	}
	if fulfillment.requests[0].SafePrice != "2.80" {
		t.Fatalf("pending 报价必须回退固定保护价: %q", fulfillment.requests[0].SafePrice)
	}
	if finishErr := store.Automation.FinishExternalPriceQuotes(ctx, task.OrderID, "adjusted", ""); finishErr != nil { // finishErr 是模拟改价成功的报价收口结果。
		t.Fatal(finishErr)
	}
	if _, sendErr := executor.sendExternalFulfillment(ctx, task, action, config); sendErr != nil { // sendErr 是 adjusted 报价下的采购发送结果。
		t.Fatal(sendErr)
	}
	if fulfillment.requests[1].SafePrice != "3.10" {
		t.Fatalf("改价成功后应使用订单级动态保护价: %q", fulfillment.requests[1].SafePrice)
	}
}

// TestDirectPaymentChecksMinimumProfitBeforePurchase 验证买家直接付款时先查实时成本，利润不足则不创建货源订单并立即引导重拍。
func TestDirectPaymentChecksMinimumProfitBeforePurchase(t *testing.T) {
	// store 和 cleanup 提供隔离测试数据库及关闭函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本次规则、订单和履约预检共用的测试上下文。
	ctx := context.Background()
	// owner 和 ownerErr 是测试账号所属管理员及读取错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// 买家实付 13.83 元，实时成本 13.64 元再加最低利润 0.20 元，已超过实付。
	ruleID, createErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: owner.ID, CookieID: "cid", ItemID: "item-direct-profit",
		Name: "直接付款利润预检", TriggerType: TriggerOrderPaid, Enabled: true,
		ConfigJSON: `{"fulfillment_failure_notice_enabled":true,"fulfillment_safe_price_notice_text":"当前最新价格 ¥{price}，请退款后重新拍下"}`,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, DeliveryCount: 1, Enabled: true, SortOrder: 1,
				ConfigJSON: `{"source_type":"external","instance_id":8,"goods_id":4683,"safe_price":"14.00","pending_price_enabled":true,"fixed_markup":"0.40","minimum_profit":"0.20"}`},
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2},
		}})
	if createErr != nil || ruleID <= 0 {
		t.Fatalf("创建直接付款测试规则失败: id=%d err=%v", ruleID, createErr)
	}
	// fulfillment 返回导致最低利润不足的实时价格，并记录是否误调采购。
	fulfillment := &externalFulfillmentStub{
		product: ExternalProductQuote{Price: "13.64", CanBuy: true},
		result:  ExternalFulfillmentResult{State: "succeeded", Cards: []string{"SHOULD-NOT-BUY"}},
	}
	// sender 接收应立即发送的退款重拍引导。
	sender := &testSender{}
	// platform 记录利润拦截后是否误确认闲鱼发货。
	platform := &fakeMTop{}
	// center 注入实付订单详情、实时报价和买家消息发送能力。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop: platform, OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "13.83", OrderStatus: "pending_ship"}},
		ExternalFulfillment: fulfillment,
	})
	// task 是没有成功改价快照的买家直接付款事件。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "order-direct-profit",
		ItemID: "item-direct-profit", BuyerID: "buyer", ChatID: "chat"}
	// handleErr 是预期的本地利润拦截结果。
	if handleErr := center.HandleTask(ctx, task); handleErr == nil {
		t.Fatal("利润不足的直接付款订单不应被当作发货成功")
	}
	if len(fulfillment.requests) != 0 || platform.consignCalls != 0 {
		t.Fatalf("利润不足必须在采购前拦截: purchases=%d consign=%d", len(fulfillment.requests), platform.consignCalls)
	}
	if len(sender.texts) != 1 || sender.texts[0] != "当前最新价格 ¥14.04，请退款后重新拍下" {
		t.Fatalf("利润拦截应立即发送最新价格和重拍引导: %v", sender.texts)
	}
	// nextRetryAt 为零证明本地已确认的利润不足不会再重试采购。
	var attemptCount int
	// nextRetryAt 是运行下次自动恢复时间，本地利润拦截必须为零。
	var nextRetryAt int64
	// queryErr 是读取运行尝试次数和恢复时间的错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT attempt_count,next_retry_at FROM automation_runs WHERE order_id=?`, task.OrderID).Scan(&attemptCount, &nextRetryAt); queryErr != nil {
		t.Fatal(queryErr)
	}
	if attemptCount != 1 || nextRetryAt != 0 {
		t.Fatalf("利润不足应立即终止且不重试: attempts=%d next_retry_at=%d", attemptCount, nextRetryAt)
	}
}

// TestDirectPaymentPreflightSkipsAcceptedExternalOrder 验证恢复已受理的外部订单时只查原单，不被新价格预检中断。
func TestDirectPaymentPreflightSkipsAcceptedExternalOrder(t *testing.T) {
	// store 和 cleanup 提供含货源表的隔离数据库及关闭函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是创建规则、原外部单和执行预检的共用上下文。
	ctx := context.Background()
	// owner 和 ownerErr 是已受理外部单归属的测试用户及读取错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// ruleID 和 createErr 是启用实时跟价的付款规则主键及创建错误。
	ruleID, createErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: owner.ID, CookieID: "cid", ItemID: "item-existing-order",
		Name: "已受理外部单", TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, DeliveryCount: 1, Enabled: true,
			ConfigJSON: `{"source_type":"external","instance_id":8,"goods_id":4683,"safe_price":"14.00","pending_price_enabled":true,"fixed_markup":"0.40","minimum_profit":"0.20"}`}}})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// rule 和 ruleErr 是包含稳定动作 ID 的持久化规则及读取错误。
	rule, ruleErr := store.Automation.Get(ctx, ruleID)
	if ruleErr != nil || rule == nil || len(rule.Actions) != 1 {
		t.Fatalf("读取已受理外单规则失败: rule=%+v err=%v", rule, ruleErr)
	}
	// insertErr 是构造已配置货源实例时的数据库错误。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO fulfillment_instances(id,public_id,user_id,name,provider,base_url,merchant_user_id,api_key) VALUES(8,'existing-source',?,'existing','kasushou_v2','https://example.invalid','merchant','secret')`, owner.ID); insertErr != nil {
		t.Fatal(insertErr)
	}
	// externalOrderNo 是模拟首次采购已经创建的稳定外部单号。
	externalOrderNo := fmt.Sprintf("xy-%s-a%d", "order-existing", rule.Actions[0].ID)
	// insertErr 是写入等待处理履约订单的数据库错误。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO fulfillment_orders(user_id,instance_id,external_order_no,xianyu_order_id,remote_goods_id,quantity,state) VALUES(?,?,?,?,?,1,'waiting')`, owner.ID, 8, externalOrderNo, "order-existing", 4683); insertErr != nil {
		t.Fatal(insertErr)
	}
	// quoteErr 证明预检若误查新价格就会失败；正确行为应在发现原单后直接返回。
	fulfillment := &externalFulfillmentStub{quoteErr: fmt.Errorf("不应重新查价")}
	// center 只注入不应被调用的实时报价替身。
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{ExternalFulfillment: fulfillment})
	// task 是调度器恢复的已付款原订单事实。
	task := Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "order-existing", ItemID: "item-existing-order", Quantity: "1", Amount: "1.00"}
	// preflightErr 应为空，表示已有外部单继续原单恢复。
	if preflightErr := center.preflightExternalDirectPayment(ctx, task, rule.Actions); preflightErr != nil {
		t.Fatalf("已存在的外部订单必须继续查原单: %v", preflightErr)
	}
}

// TestExternalFulfillmentFailureNoticeAfterRetriesExhausted 验证保护价采购失败只在三次自动尝试结束后提示买家一次，并且不确认闲鱼发货。
func TestExternalFulfillmentFailureNoticeAfterRetriesExhausted(t *testing.T) {
	// store、cleanup 保存隔离数据库和测试结束后的清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是规则、运行恢复和消息防重共用的测试上下文。
	ctx := context.Background()
	// owner 是测试闲鱼账号所属用户。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// ruleID 是开启最终失败提示的外部货源付款规则主键。
	ruleID, createErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: owner.ID, CookieID: "cid", ItemID: "item-failure",
		Name: "保护价失败提示", TriggerType: TriggerOrderPaid, Enabled: true,
		ConfigJSON: `{"fulfillment_failure_notice_enabled":true,"fulfillment_safe_price_notice_text":"最新总价 ¥{price}，请退款后重新拍下","fulfillment_failure_notice_text":"订单 {order_id} 正在人工核实，请勿重复下单"}`,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, DeliveryCount: 1, Enabled: true, SortOrder: 1,
				ConfigJSON: `{"source_type":"external","instance_id":8,"goods_id":40863,"safe_price":"2.80","pending_price_enabled":true,"fixed_markup":"0.50","minimum_profit":"0.20"}`},
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2},
		}})
	if createErr != nil {
		t.Fatal(createErr)
	}
	if ruleID <= 0 {
		t.Fatal("外部货源失败通知规则未创建")
	}
	// rule 是带持久化动作 ID 的完整规则，用于直接验证第二类货源站异常提示。
	rule, ruleErr := store.Automation.Get(ctx, ruleID)
	if ruleErr != nil || rule == nil {
		t.Fatalf("读取失败通知规则失败: rule=%+v err=%v", rule, ruleErr)
	}
	// fulfillment 模拟供应商因当前价格超过采购保护价而明确拒绝每次采购。
	fulfillment := &externalFulfillmentStub{product: ExternalProductQuote{Price: "3.10", CanBuy: true}, fulfillErr: fmt.Errorf("%w: 当前价格超过保护价", ErrExternalSafePriceExceeded)}
	// platform 记录确认发货调用；采购失败时调用次数必须保持为零。
	platform := &fakeMTop{}
	// sender 接收首次订单受理提示和第三次采购失败后的买家人工处理提示。
	sender := &testSender{}
	// notifier 记录原有管理员失败通知，确保新增买家提示没有替代运维告警。
	notifier := &recordingNotifier{}
	// center 注入订单详情、外部货源、消息发送、管理员通知和确认发货能力。
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop: platform, OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "9.90", OrderStatus: "pending_ship"}},
		ExternalFulfillment: fulfillment, Notifier: notifier,
	})
	// task 是买家直接付款、没有订单级动态保护价的真实付款事件。
	task := Task{Source: "ws", AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "order-safe-price",
		ItemID: "item-failure", BuyerID: "buyer", ChatID: "chat", Quantity: "1"}
	if firstErr := center.HandleTask(ctx, task); firstErr == nil { // firstErr 是首次保护价采购拒绝，应进入安全重试。
		t.Fatal("首次保护价采购失败不应被当作成功")
	}
	// processingNotice 是采购开始前按订单只发送一次的固定受理提示。
	processingNotice := "亲，已收到您的订单order-safe-price\n正在为您发货，请稍候～\n预计1-2分钟，发货成功会第一时间通知您，感谢耐心等待！"
	if len(sender.texts) != 1 || sender.texts[0] != processingNotice {
		t.Fatalf("首次采购应只发送订单受理提示: %v", sender.texts)
	}
	for retryIndex := 0; retryIndex < 2; retryIndex++ { // retryIndex 表示剩余两次自动恢复尝试的下标。
		if _, updateErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET next_retry_at=0 WHERE order_id=?`, task.OrderID); updateErr != nil { // updateErr 是测试加速重试时间的数据库错误。
			t.Fatal(updateErr)
		}
		_ = NewScheduler(center).runRecoveryTasks(ctx)
		if retryIndex == 0 && len(sender.texts) != 1 {
			t.Fatalf("第二次失败不得重复受理提示或提前发送失败通知: %v", sender.texts)
		}
	}
	// expectedNotice 是按实时货源价 3.10 元加固定加价 0.50 元计算的新订单总价和重新下单引导。
	expectedNotice := "最新总价 ¥3.60，请退款后重新拍下"
	if len(sender.texts) != 2 || sender.texts[1] != expectedNotice {
		t.Fatalf("重试耗尽后买家提示异常: %v", sender.texts)
	}
	if len(fulfillment.requests) != 3 || platform.consignCalls != 0 {
		t.Fatalf("应采购三次且绝不确认发货: purchases=%d consign=%d", len(fulfillment.requests), platform.consignCalls)
	}
	if len(notifier.messages()) != 3 {
		t.Fatalf("管理员仍应收到每次失败通知: %v", notifier.messages())
	}
	// duplicateErr 是重复扫描结果；已发送防重记录必须阻止第二次买家提示。
	duplicateErr := NewScheduler(center).runRecoveryTasks(ctx)
	if duplicateErr != nil || len(sender.texts) != 2 {
		t.Fatalf("最终失败提示不应重复: texts=%v err=%v", sender.texts, duplicateErr)
	}
	// supplierTask 模拟另一笔非保护价货源站异常，必须选择人工核实文案而不是重新报价。
	supplierTask := task
	supplierTask.OrderID = "order-supplier-error"
	if noticeErr := center.sendExternalFulfillmentFailureNotice(ctx, supplierTask, *rule, false); noticeErr != nil { // noticeErr 是货源站异常提示的发送结果。
		t.Fatal(noticeErr)
	}
	if len(sender.texts) != 3 || sender.texts[2] != "订单 order-supplier-error 正在人工核实，请勿重复下单" {
		t.Fatalf("货源站异常应使用人工核实提示: %v", sender.texts)
	}
	// unavailableTask 模拟保护价拦截后商品已经不可采购，系统不得向买家发送不可靠的新价格。
	fulfillment.product = ExternalProductQuote{Price: "3.20", CanBuy: false}
	// unavailableTask 是需要验证报价回退策略的另一笔独立订单。
	unavailableTask := task
	unavailableTask.OrderID = "order-price-unavailable"
	if noticeErr := center.sendExternalFulfillmentFailureNotice(ctx, unavailableTask, *rule, true); noticeErr != nil { // noticeErr 是重新报价失败后回退人工核实提示的结果。
		t.Fatal(noticeErr)
	}
	if len(sender.texts) != 4 || sender.texts[3] != "订单 order-price-unavailable 正在人工核实，请勿重复下单" {
		t.Fatalf("最新报价不可用时应回退人工核实提示: %v", sender.texts)
	}
}
