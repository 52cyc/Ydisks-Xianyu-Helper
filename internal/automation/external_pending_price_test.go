package automation

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

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
}

// TestExternalPendingPriceAdjustsAndPersistsDynamicSafePrice 验证实时单价、可配置固定加价和最低利润生成订单级改价及采购保护价。
func TestExternalPendingPriceAdjustsAndPersistsDynamicSafePrice(t *testing.T) {
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
	// task 是买家已拍下但尚未付款的真实订单事件。
	task := Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "order-price", ItemID: "item-price", BuyerID: "buyer", ChatID: "chat"}
	if handleErr := center.HandleTask(ctx, task); handleErr != nil { // handleErr 是首次待付款跟价处理结果。
		t.Fatal(handleErr)
	}
	if platform.adjustCalls != 1 || platform.adjustCentsIn != 660 {
		t.Fatalf("目标价格应为 (2.80+0.50)*2=6.60: calls=%d cents=%d", platform.adjustCalls, platform.adjustCentsIn)
	}
	if len(sender.texts) != 3 || sender.texts[2] != "最新总价 ¥6.60，数量 2" {
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
	if len(sender.texts) != 3 {
		t.Fatalf("重复待付款事件不应再次发送改价通知: %#v", sender.texts)
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
