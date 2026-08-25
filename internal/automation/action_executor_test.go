package automation

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// blockingAutomationSender 在发送入口阻塞，用于验证库存锁不会覆盖外部消息 I/O。
type blockingAutomationSender struct {
	// calls 记录进入发送入口的并发调用次数。
	calls int32
	// firstEntered 通知第一个发送调用已经拿到卡密并进入外部 I/O。
	firstEntered chan struct{}
	// secondEntered 通知第二个发送调用也已经进入外部 I/O。
	secondEntered chan struct{}
	// release 允许被阻塞的外部发送继续完成。
	release chan struct{}
}

// blockingSenderProvider 为并发测试提供阻塞发送器。
type blockingSenderProvider struct {
	// sender 保存待注入的阻塞发送器。
	sender MessageSender
}

// Sender 返回并发测试使用的阻塞发送器。
func (p blockingSenderProvider) Sender(string) (MessageSender, bool) {
	return p.sender, true
}

// SendText 模拟一个可控的慢速外部发送。
func (s *blockingAutomationSender) SendText(context.Context, string, string, string) error {
	// callNumber 保存本次发送在测试中的并发序号。
	callNumber := atomic.AddInt32(&s.calls, 1)
	if callNumber == 1 {
		close(s.firstEntered)
	}
	if callNumber == 2 {
		close(s.secondEntered)
	}
	<-s.release
	return nil
}

// SendImage 满足消息发送接口；数据卡测试不会调用图片发送。
func (s *blockingAutomationSender) SendImage(context.Context, string, string, string, int64, int, int) error {
	return nil
}

// UpdateCookie 满足消息发送接口；本测试不需要更新运行时 Cookie。
func (s *blockingAutomationSender) UpdateCookie(string) {}

// TestAutomationActionExecutorPreservesMessageNotSent 验证动作执行器保留“确定未发送”错误，供运行协调器安全重试。
func TestAutomationActionExecutorPreservesMessageNotSent(t *testing.T) {
	// sender 是返回确定未发送错误的测试发送器。
	sender := &testSender{err: fmt.Errorf("%w: websocket 尚未就绪", ErrMessageNotSent)}
	// executor 是仅注入消息发送器的动作执行器。
	executor := automationActionExecutor{senders: testSenderProvider{sender: sender}}
	// sent 是动作执行器报告的已发送数量。
	sent, err := executor.executeAction(context.Background(), Task{
		AccountID: "cid",
		ChatID:    "chat",
		BuyerID:   "buyer",
	}, db.AutomationAction{ActionType: ActionSendText, MessageTemplate: "hello"})
	if sent != 0 || !errors.Is(err, ErrMessageNotSent) {
		t.Fatalf("确定未发送错误未保留: sent=%d err=%v", sent, err)
	}
}

// apiCardFetcherStub 记录 API 卡发货请求，供逐单位执行测试使用。
type apiCardFetcherStub struct {
	// requests 保存自动化执行器提交的每个发货单位上下文。
	requests []APICardRequest
}

// externalFulfillmentStub 记录自动化中心提交给外部货源的幂等采购请求。
type externalFulfillmentStub struct {
	// requests 保存历次采购请求。
	requests []ExternalFulfillmentRequest
	// result 是每次采购返回的统一履约结果。
	result ExternalFulfillmentResult
}

// Fulfill 记录请求并返回预设卡密结果。
func (s *externalFulfillmentStub) Fulfill(_ context.Context, request ExternalFulfillmentRequest) (ExternalFulfillmentResult, error) {
	s.requests = append(s.requests, request)
	return s.result, nil
}

// TestSendExternalFulfillmentUsesStableOrderNumber 验证外部采购按闲鱼订单和动作 ID 生成稳定单号并发送卡密。
func TestSendExternalFulfillmentUsesStableOrderNumber(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是数据库和动作执行器共用的测试上下文。
	ctx := context.Background()
	// admin 是测试闲鱼账号的所有者。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.CreateOwned(ctx, "external-account", "cookie", admin.ID); err != nil { // err 是测试账号归属写入错误。
		t.Fatal(err)
	}
	// fulfillment 是返回两条卡密的外部履约替身。
	fulfillment := &externalFulfillmentStub{result: ExternalFulfillmentResult{State: "succeeded", Cards: []string{"CODE-1", "CODE-2"}}}
	// sender 保存发送给闲鱼买家的外部卡密。
	sender := &testSender{}
	// executor 是注入货源适配器与在线发送器的动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}, externalFulfillment: func() ExternalFulfillment { return fulfillment }}
	// task 是带购买数量的闲鱼付款订单。
	task := Task{AccountID: "external-account", OrderID: "XY-ORDER-9", ChatID: "chat", BuyerID: "buyer", Quantity: "2", TriggerType: TriggerOrderPaid}
	// action 是每件采购一份的外部商品发货动作。
	action := db.AutomationAction{ID: 17, ActionType: ActionSendCard, DeliveryCount: 1, ConfigJSON: `{"source_type":"external","instance_id":3,"goods_id":4366,"safe_price":"9.90"}`}
	// sent、sendErr 保存执行结果。
	sent, sendErr := executor.sendCard(ctx, task, action)
	if sendErr != nil || sent != 2 || len(sender.texts) != 2 || len(fulfillment.requests) != 1 {
		t.Fatalf("外部卡密履约失败: sent=%d texts=%v requests=%+v err=%v", sent, sender.texts, fulfillment.requests, sendErr)
	}
	// request 是本次提交给供应商的采购参数。
	request := fulfillment.requests[0]
	if request.ExternalOrderNo != "xy-XY-ORDER-9-a17" || request.GoodsID != 4366 || request.Quantity != 2 || request.SafePrice != "9.90" {
		t.Fatalf("外部采购参数错误: %+v", request)
	}
}

// TestSendExternalFulfillmentMarksPendingState 验证供应站处理中状态会进入长轮询分类，同时保留动作明确未执行语义。
func TestSendExternalFulfillmentMarksPendingState(t *testing.T) {
	// store、cleanup 是包含账号归属关系的测试数据库及释放函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库和履约动作共享的上下文。
	ctx := context.Background()
	// admin、ownerErr 是测试账号所有者及查询错误。
	admin, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	if createErr := store.Cookies.CreateOwned(ctx, "pending-account", "cookie", admin.ID); createErr != nil { // createErr 是测试账号写入错误。
		t.Fatal(createErr)
	}
	// fulfillment 模拟已经受理但仍在处理的供应站订单。
	fulfillment := &externalFulfillmentStub{result: ExternalFulfillmentResult{State: "waiting"}}
	// executor 只需注入货源能力；等待状态不会调用闲鱼消息发送器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: &testSender{}}, externalFulfillment: func() ExternalFulfillment { return fulfillment }}
	// task 是用于生成幂等外部单号的稳定闲鱼订单事实。
	task := Task{AccountID: "pending-account", OrderID: "XY-PENDING-1", Quantity: "1", TriggerType: TriggerOrderPaid}
	// action 是指向测试货源商品的外部采购动作配置。
	action := db.AutomationAction{ID: 19, ActionType: ActionSendCard, DeliveryCount: 1, ConfigJSON: `{"source_type":"external","instance_id":3,"goods_id":5419}`}
	// sent、sendErr 是动作返回的确认发送数和等待错误。
	sent, sendErr := executor.sendCard(ctx, task, action)
	if sent != 0 || !errors.Is(sendErr, errExternalFulfillmentPending) || !errors.Is(sendErr, errActionNotPerformed) {
		t.Fatalf("等待状态分类错误: sent=%d err=%v", sent, sendErr)
	}
}

// TestSendExternalFulfillmentRendersOrderAttach 验证直充字段使用当前闲鱼订单动态字段且缺失时不会采购。
func TestSendExternalFulfillmentRendersOrderAttach(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本次直充测试使用的上下文。
	ctx := context.Background()
	// admin、err 是测试账号所有者与查询错误。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err /* err 是创建测试闲鱼账号的错误。 */ := store.Cookies.CreateOwned(ctx, "recharge-account", "cookie", admin.ID); err != nil {
		t.Fatal(err)
	}
	// fulfillment 记录实际提交给货源站的动态直充参数。
	fulfillment := &externalFulfillmentStub{result: ExternalFulfillmentResult{State: "succeeded", RechargeInfo: "充值成功"}}
	// executor 是注入测试货源和消息发送器的动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: &testSender{}}, externalFulfillment: func() ExternalFulfillment { return fulfillment }}
	// action 把智客字段 1 映射到闲鱼订单的“充值账号”。
	action := db.AutomationAction{ID: 18, ActionType: ActionSendCard, DeliveryCount: 1, ConfigJSON: `{"source_type":"external","instance_id":3,"goods_id":4994,"attach":{"1":"{order_field:充值账号}"}}`}
	// task 是已从闲鱼订单详情取得“充值账号”的付款订单。
	task := Task{AccountID: "recharge-account", OrderID: "XY-RECHARGE-1", ChatID: "chat", BuyerID: "buyer", Quantity: "1", OrderFields: map[string]string{"充值账号": "13800000000"}, TriggerType: TriggerOrderPaid}
	if _, sendErr /* sendErr 是动态字段采购和发送错误。 */ := executor.sendCard(ctx, task, action); sendErr != nil {
		t.Fatal(sendErr)
	}
	if len(fulfillment.requests) != 1 || fulfillment.requests[0].Attach["1"] != "13800000000" {
		t.Fatalf("直充参数未绑定闲鱼订单字段: %+v", fulfillment.requests)
	}
	// missingTask 缺少联系电话，必须在调用供应商前失败。
	missingTask := task
	missingTask.OrderID = "XY-RECHARGE-2"
	missingTask.OrderFields = nil
	if _, sendErr /* sendErr 是缺少动态字段时的预期错误。 */ := executor.sendCard(ctx, missingTask, action); sendErr == nil || len(fulfillment.requests) != 1 {
		t.Fatalf("缺少闲鱼联系电话时不应采购: requests=%+v err=%v", fulfillment.requests, sendErr)
	}
}

// TestRechargeChatWorkflowRequiresConfirmation 验证直充账号只在买家明确确认后注入采购任务。
func TestRechargeChatWorkflowRequiresConfirmation(t *testing.T) {
	// store、cleanup 保存带最新迁移的测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// sender 记录索取、确认和开始充值的固定消息。
	sender := &testSender{}
	// center 只注入聊天状态机所需的数据库和在线发送器。
	center := &Center{store: store, actions: automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}}}
	// task 是已付款但还没有直充账号的闲鱼订单。
	task := Task{AccountID: "recharge-chat-account", OrderID: "XY-CHAT-1", ChatID: "chat-1", BuyerID: "buyer-1@goofish", TriggerType: TriggerOrderPaid}
	// action 把货源字段 1 配置为聊天收集。
	action := db.AutomationAction{ID: 28, ActionType: ActionSendCard, ConfigJSON: `{"source_type":"external","instance_id":3,"goods_id":4994,"attach":{"1":"{chat_input}"}}`}
	prepared, waiting, err := center.prepareRechargeChatInput(context.Background(), task, 91, action) // prepared、waiting、err 是首次收集前的任务、等待标记和错误。
	if err != nil || !waiting || len(sender.texts) != 1 || sender.texts[0] != rechargeChatPrompt {
		t.Fatalf("首次索取状态错误: waiting=%v texts=%v err=%v", waiting, sender.texts, err)
	}
	if handled, inputErr /* handled、inputErr 表示账号文本是否被消费及其处理错误。 */ := center.HandleRechargeChat(context.Background(), RechargeChatMessage{AccountID: task.AccountID, ChatID: task.ChatID, BuyerID: "buyer-1", Text: "13800000000"}); inputErr != nil || !handled {
		t.Fatalf("账号输入未被直充状态机消费: handled=%v err=%v", handled, inputErr)
	}
	if len(sender.texts) != 2 || sender.texts[1] != "请确认充值账号：13800000000\n回复“确认”开始充值，回复“重填”重新输入。" {
		t.Fatalf("原值确认文案错误: %v", sender.texts)
	}
	if handled, remindErr /* handled、remindErr 表示待确认阶段的其他文本是否被截止及其错误。 */ := center.HandleRechargeChat(context.Background(), RechargeChatMessage{AccountID: task.AccountID, ChatID: task.ChatID, BuyerID: "buyer-1", Text: "看看"}); remindErr != nil || !handled {
		t.Fatalf("待确认提醒失败: handled=%v err=%v", handled, remindErr)
	}
	if len(sender.texts) != 3 || sender.texts[2] != sender.texts[1] {
		t.Fatalf("重复确认仍应回显原账号: %v", sender.texts)
	}
	if _, waiting, err = center.prepareRechargeChatInput(context.Background(), prepared, 91, action); err != nil || !waiting {
		t.Fatalf("未确认前不应放行采购: waiting=%v err=%v", waiting, err)
	}
	if handled, confirmErr /* handled、confirmErr 表示确认文本是否被消费及其处理错误。 */ := center.HandleRechargeChat(context.Background(), RechargeChatMessage{AccountID: task.AccountID, ChatID: task.ChatID, BuyerID: "buyer-1@goofish", Text: "确认"}); confirmErr != nil || !handled {
		t.Fatalf("确认消息处理失败: handled=%v err=%v", handled, confirmErr)
	}
	prepared, waiting, err = center.prepareRechargeChatInput(context.Background(), prepared, 91, action)
	if err != nil || waiting || prepared.OrderFields[rechargeChatField("1")] != "13800000000" {
		t.Fatalf("确认后未注入原始账号: waiting=%v fields=%v err=%v", waiting, prepared.OrderFields, err)
	}
	rendered, renderErr := renderExternalAttach(map[string]string{"1": rechargeChatToken}, prepared) // rendered、renderErr 是最终货源字段和渲染错误。
	if renderErr != nil || rendered["1"] != "13800000000" {
		t.Fatalf("货源 attach 参数错误: attach=%v err=%v", rendered, renderErr)
	}
}

// Fetch 返回按单位序号生成的测试卡密，不执行真实网络请求。
func (s *apiCardFetcherStub) Fetch(_ context.Context, request APICardRequest) (APICardResult, error) {
	s.requests = append(s.requests, request)
	return APICardResult{Content: fmt.Sprintf("API-CODE-%d", request.UnitIndex)}, nil
}

// TestSendAPICardDeliversEachUnit 验证规则数量乘订单数量后逐单位获取并立即发送 API 卡密。
func TestSendAPICardDeliversEachUnit(t *testing.T) {
	// store、cleanup 保存测试数据库和清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存数据库与执行器共用的测试上下文。
	ctx := context.Background()
	// admin 是测试卡券的所有者。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// cardID 保存测试 API 卡券组标识。
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "API", Type: "api", APIConfig: `{"url":"https://example.com"}`, Enabled: true, UserID: admin.ID})
	if err != nil {
		t.Fatal(err)
	}
	// fetcher 记录每个单位的 API 请求上下文。
	fetcher := &apiCardFetcherStub{}
	// sender 保存已经发送给买家的 API 卡密文本。
	sender := &testSender{}
	// executor 是注入 API 客户端和在线发送器的动作执行器。
	executor := automationActionExecutor{store: store, senders: testSenderProvider{sender: sender}, apiFetcher: func() APICardFetcher { return fetcher }}
	// sent、sendErr 保存四个单位的执行统计和最终错误。
	sent, sendErr := executor.sendCard(ctx, Task{AccountID: "cid", OrderID: "order", ChatID: "chat", BuyerID: "buyer", Quantity: "2", TriggerType: TriggerOrderPaid}, db.AutomationAction{ID: 8, ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 2, ConfigJSON: "{}"})
	if sendErr != nil || sent != 4 || len(fetcher.requests) != 4 || len(sender.texts) != 4 {
		t.Fatalf("API 卡未逐单位发送 sent=%d requests=%d texts=%v err=%v", sent, len(fetcher.requests), sender.texts, sendErr)
	}
	// index、request 分别表示发货单位序号和对应的 API 请求上下文。
	for index, request := range fetcher.requests {
		if request.UnitIndex != index+1 || request.TotalUnits != 4 {
			t.Fatalf("单位上下文错误 index=%d request=%+v", index, request)
		}
	}
}

// TestSendDataCardReleasesInventoryLockBeforeExternalSend 验证第二个库存操作不会等待第一个外部发送完成。
func TestSendDataCardReleasesInventoryLockBeforeExternalSend(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存本测试共用的上下文。
	ctx := context.Background()
	// admin 保存创建卡券组所需的管理员用户。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// cardID 保存包含两条库存数据的卡券组标识。
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "concurrent-data", Type: "data", DataContent: "secret-1\nsecret-2", Enabled: true, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// sender 保存可控制外部发送完成时机的测试发送器。
	sender := &blockingAutomationSender{
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		release:       make(chan struct{}),
	}
	// center 保存注入测试发送器的自动化中心。
	center := New(store, blockingSenderProvider{sender: sender}, nil)
	// result 保存两个并发动作的返回结果。
	result := make(chan error, 2)
	// task 保存两个动作共用的订单消息上下文。
	task := Task{AccountID: "cid", ChatID: "chat", BuyerID: "buyer"}
	// action 保存每次发送一条数据卡密的动作配置。
	action := db.AutomationAction{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`}
	go func() {
		// runErr 保存第一个并发卡密动作的执行错误。
		_, runErr := center.sendCard(ctx, task, action)
		result <- runErr
	}()
	select {
	case <-sender.firstEntered:
	case <-time.After(time.Second):
		t.Fatal("第一个发送调用未进入外部 I/O")
	}
	go func() {
		// runErr 保存第二个并发卡密动作的执行错误。
		_, runErr := center.sendCard(ctx, task, action)
		result <- runErr
	}()
	select {
	case <-sender.secondEntered:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("第二个库存操作被第一个外部发送阻塞")
	}
	close(sender.release)
	for range 2 {
		// runErr 保存并发动作收口时的执行错误。
		if runErr := <-result; runErr != nil {
			t.Fatalf("并发数据卡发送失败: %v", runErr)
		}
	}
}
