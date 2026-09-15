package automation

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// listingPriceQuoteStub 记录后台报价开始时间，并可为指定商品返回错误。
type listingPriceQuoteStub struct {
	// mu 保护 starts 和 goodsIDs，测试断言不会与扫描 goroutine 发生数据竞争。
	mu sync.Mutex
	// starts 保存每次进入货源报价边界的墙钟时间。
	starts []time.Time
	// goodsIDs 保存与 starts 相同顺序的远程商品标识。
	goodsIDs []int64
	// quoteErrors 保存需要模拟供应站限频或业务失败的商品错误。
	quoteErrors map[int64]error
	// unavailableGoods 标记供应站查询成功但当前不允许采购的商品。
	unavailableGoods map[int64]bool
	// firstStarted 在首次报价开始后通知取消测试；由首次调用方关闭。
	firstStarted chan struct{}
}

// listingPriceEditorStub 复用测试 MTOP 客户端并记录商品页改价请求。
type listingPriceEditorStub struct {
	// fakeMTop 提供价格扫描未使用的 MTOP 客户端方法。
	*fakeMTop
	// itemIDs 保存商品页改价的闲鱼商品标识。
	itemIDs []string
	// prices 保存商品页改价的目标分值。
	prices []int64
}

// UpdateItemPriceContext 记录商品页改价并模拟平台明确成功。
func (stub *listingPriceEditorStub) UpdateItemPriceContext(_ context.Context, _ string, itemID string, priceCents int64) (bool, []string, string, error) {
	stub.itemIDs = append(stub.itemIDs, itemID)
	stub.prices = append(stub.prices, priceCents)
	return true, []string{"SUCCESS::调用成功"}, "", nil
}

// QuoteProduct 记录报价开始事实并返回固定一元采购价，不创建远程订单。
func (stub *listingPriceQuoteStub) QuoteProduct(_ context.Context, _ int64, _ int64, goodsID int64) (ExternalProductQuote, error) {
	stub.mu.Lock()
	stub.starts = append(stub.starts, time.Now())
	stub.goodsIDs = append(stub.goodsIDs, goodsID)
	// callCount 是包含当前请求的累计报价次数。
	callCount := len(stub.starts)
	// quoteErr 是当前商品预设的供应站错误。
	quoteErr := stub.quoteErrors[goodsID]
	// canBuy 是当前商品的可采购状态，默认可采购以保持其他测试语义。
	canBuy := !stub.unavailableGoods[goodsID]
	stub.mu.Unlock()
	if callCount == 1 && stub.firstStarted != nil {
		close(stub.firstStarted)
	}
	return ExternalProductQuote{Price: "1.00", CanBuy: canBuy}, quoteErr
}

// Fulfill 满足自动化依赖接口；价格扫描不会调用采购入口。
func (*listingPriceQuoteStub) Fulfill(context.Context, ExternalFulfillmentRequest) (ExternalFulfillmentResult, error) {
	return ExternalFulfillmentResult{}, errors.New("价格同步测试不应创建采购单")
}

// snapshot 返回不会再被调用方修改的报价时间和商品顺序副本。
func (stub *listingPriceQuoteStub) snapshot() ([]time.Time, []int64) {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	// starts、goodsIDs 是供断言使用的隔离副本。
	starts := append([]time.Time(nil), stub.starts...)
	// goodsIDs 是与时间副本相同顺序的远程商品标识副本。
	goodsIDs := append([]int64(nil), stub.goodsIDs...)
	return starts, goodsIDs
}

// createListingPriceSyncRules 创建价格已等于目标值的商品和启用规则，避免测试触发闲鱼改价。
func createListingPriceSyncRules(t *testing.T, store *db.Store, goodsIDs ...int64) {
	t.Helper()
	// ctx 是本地数据库准备阶段使用的测试上下文。
	ctx := context.Background()
	// owner 是隔离数据库默认管理员，用于满足规则归属约束。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	if cookieErr := store.Cookies.CreateOwned(ctx, "paced-account", "test-cookie", owner.ID); cookieErr != nil { // cookieErr 是商品和规则外键所需的测试账号准备错误。
		t.Fatal(cookieErr)
	}
	for index, goodsID := range goodsIDs { // index、goodsID 是当前商品顺序和对应远程商品标识。
		// itemID 是保证数据库排序与传入商品顺序一致的闲鱼商品标识。
		itemID := fmt.Sprintf("paced-item-%02d", index)
		if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "paced-account", ItemID: itemID, ItemTitle: itemID, ItemPrice: "1.00"}); itemErr != nil { // itemErr 是本地商品准备错误。
			t.Fatal(itemErr)
		}
		// config 保存当前规则的单规格外部货源价格同步参数。
		config := fmt.Sprintf(`{"source_type":"external","instance_id":8,"goods_id":%d,"price_sync_enabled":true}`, goodsID)
		if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: owner.ID, CookieID: "paced-account", ItemID: itemID, Name: itemID, TriggerType: TriggerOrderPaid, Enabled: true, Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, DeliveryCount: 1, Enabled: true, ConfigJSON: config}}}); ruleErr != nil { // ruleErr 是价格同步规则准备错误。
			t.Fatal(ruleErr)
		}
	}
}

// TestScanExternalListingPricesPacesQuotesAndContinuesAfterError 验证报价串行限速且单条失败不阻断后续商品。
func TestScanExternalListingPricesPacesQuotesAndContinuesAfterError(t *testing.T) {
	// store、cleanup 是隔离数据库和测试结束后的连接清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	createListingPriceSyncRules(t, store, 101, 102, 103)
	// fulfillment 让中间商品模拟供应站限频，前后商品仍正常报价。
	fulfillment := &listingPriceQuoteStub{quoteErrors: map[int64]error{102: errors.New("请求频繁")}}
	// scheduler 使用短测试间隔验证生产同一节流分支而不让测试固定等待一秒。
	scheduler := NewScheduler(NewWithDependencies(store, nil, nil, CenterDependencies{ExternalFulfillment: fulfillment}))
	scheduler.externalListingQuoteInterval = 20 * time.Millisecond
	scheduler.scanExternalListingPrices(context.Background())
	// starts、goodsIDs 是扫描完成后的稳定报价记录。
	starts, goodsIDs := fulfillment.snapshot()
	if len(starts) != 3 || fmt.Sprint(goodsIDs) != "[101 102 103]" {
		t.Fatalf("报价顺序或失败后继续行为错误: goods=%v starts=%d", goodsIDs, len(starts))
	}
	for index := 1; index < len(starts); index++ { // index 是当前与前一次报价比较的记录位置。
		if gap := starts[index].Sub(starts[index-1]); gap < 15*time.Millisecond { // gap 是相邻报价实际开始间隔，允许少量计时器精度余量。
			t.Fatalf("第 %d 次报价间隔过短: %s", index+1, gap)
		}
	}
}

// TestScanExternalListingPricesQuotesDuplicateItemOnce 验证同账号商品的低优先级规则不会再次访问供应站。
func TestScanExternalListingPricesQuotesDuplicateItemOnce(t *testing.T) {
	// store、cleanup 是隔离数据库和测试结束后的连接清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	createListingPriceSyncRules(t, store, 301)
	// ctx 是补充同商品规则时使用的测试上下文。
	ctx := context.Background()
	// owner 是第二条规则需要的同一所有者。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// duplicateConfig 指向不同远程商品，用于证明去重依据的是闲鱼账号商品。
	duplicateConfig := `{"source_type":"external","instance_id":8,"goods_id":302,"price_sync_enabled":true}`
	if _, ruleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: owner.ID, CookieID: "paced-account", ItemID: "paced-item-00", Name: "duplicate", TriggerType: TriggerOrderPaid, Priority: -1, Enabled: true, Actions: []db.AutomationActionInput{{ActionType: ActionSendCard, DeliveryCount: 1, Enabled: true, ConfigJSON: duplicateConfig}}}); ruleErr != nil { // ruleErr 是同商品低优先级规则准备错误。
		t.Fatal(ruleErr)
	}
	// fulfillment 记录实际到达供应站的报价次数。
	fulfillment := &listingPriceQuoteStub{}
	// scheduler 执行一轮并应在首条规则后跳过重复商品。
	scheduler := NewScheduler(NewWithDependencies(store, nil, nil, CenterDependencies{ExternalFulfillment: fulfillment}))
	scheduler.scanExternalListingPrices(ctx)
	// starts 是本轮的报价开始记录。
	starts, _ := fulfillment.snapshot()
	if len(starts) != 1 {
		t.Fatalf("同账号商品本轮应只报价一次: %d", len(starts))
	}
}

// TestScanExternalListingPricesMarksMissingProductAt9999 验证货源商品删除后远端和本地商品都调整为人工核对价。
func TestScanExternalListingPricesMarksMissingProductAt9999(t *testing.T) {
	// store、cleanup 是隔离数据库和测试结束后的连接清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	createListingPriceSyncRules(t, store, 5435)
	// fulfillment 明确返回远程商品已经不存在。
	fulfillment := &listingPriceQuoteStub{quoteErrors: map[int64]error{5435: ErrExternalProductNotFound}}
	// editor 记录应当执行一次的闲鱼商品页保护价修改。
	editor := &listingPriceEditorStub{fakeMTop: &fakeMTop{}}
	// scheduler 使用真实商品页同步编排和本地持久化路径。
	scheduler := NewScheduler(NewWithDependencies(store, nil, nil, CenterDependencies{MTop: editor, ExternalFulfillment: fulfillment}))
	scheduler.scanExternalListingPrices(context.Background())
	if len(editor.prices) != 1 || editor.prices[0] != externalProductReviewListingPriceCents || editor.itemIDs[0] != "paced-item-00" {
		t.Fatalf("货源商品删除后的改价请求异常: items=%v prices=%v", editor.itemIDs, editor.prices)
	}
	// item、itemErr 是远端改价成功后写回的本地商品价格。
	item, itemErr := store.Items.GetByCookieItem(context.Background(), "paced-account", "paced-item-00")
	if itemErr != nil || item.ItemPrice != "9999.00" {
		t.Fatalf("人工核对价未写回本地: item=%+v err=%v", item, itemErr)
	}
}

// TestScanExternalListingPricesMarksUnavailableProductAt9999 验证货源商品存在但不可采购时也会调整为人工核对价。
func TestScanExternalListingPricesMarksUnavailableProductAt9999(t *testing.T) {
	// store、cleanup 是隔离数据库和测试结束后的连接清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	createListingPriceSyncRules(t, store, 46)
	// fulfillment 返回存在但状态或库存导致不可采购的货源商品。
	fulfillment := &listingPriceQuoteStub{unavailableGoods: map[int64]bool{46: true}}
	// editor 记录应当执行一次的闲鱼商品页人工核对价修改。
	editor := &listingPriceEditorStub{fakeMTop: &fakeMTop{}}
	// scheduler 使用真实商品页同步编排和本地持久化路径。
	scheduler := NewScheduler(NewWithDependencies(store, nil, nil, CenterDependencies{MTop: editor, ExternalFulfillment: fulfillment}))
	scheduler.scanExternalListingPrices(context.Background())
	if len(editor.prices) != 1 || editor.prices[0] != externalProductReviewListingPriceCents || editor.itemIDs[0] != "paced-item-00" {
		t.Fatalf("货源商品不可采购时的改价请求异常: items=%v prices=%v", editor.itemIDs, editor.prices)
	}
	// item、itemErr 是远程改价成功后写回的本地商品价格。
	item, itemErr := store.Items.GetByCookieItem(context.Background(), "paced-account", "paced-item-00")
	if itemErr != nil || item.ItemPrice != "9999.00" {
		t.Fatalf("不可采购商品的人工核对价未写回本地: item=%+v err=%v", item, itemErr)
	}
}

// TestScanExternalListingPricesDoesNotMarkOrdinaryQuoteFailure 验证限流等普通报价失败不会触发人工核对价。
func TestScanExternalListingPricesDoesNotMarkOrdinaryQuoteFailure(t *testing.T) {
	// store、cleanup 是隔离数据库和测试结束后的连接清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	createListingPriceSyncRules(t, store, 5436)
	// fulfillment 模拟供应站限流，该错误不能被视为商品删除。
	fulfillment := &listingPriceQuoteStub{quoteErrors: map[int64]error{5436: errors.New("请求频繁，剩余3秒")}}
	// editor 记录本轮扫描是否错误调用闲鱼商品改价。
	editor := &listingPriceEditorStub{fakeMTop: &fakeMTop{}}
	// scheduler 使用真实商品页同步编排执行限流失败分支。
	scheduler := NewScheduler(NewWithDependencies(store, nil, nil, CenterDependencies{MTop: editor, ExternalFulfillment: fulfillment}))
	scheduler.scanExternalListingPrices(context.Background())
	if len(editor.prices) != 0 {
		t.Fatalf("普通报价失败不应修改闲鱼商品价格: items=%v prices=%v", editor.itemIDs, editor.prices)
	}
	// item、itemErr 是普通失败后仍应保持原价的本地商品。
	item, itemErr := store.Items.GetByCookieItem(context.Background(), "paced-account", "paced-item-00")
	if itemErr != nil || item.ItemPrice != "1.00" {
		t.Fatalf("普通报价失败误写本地价格: item=%+v err=%v", item, itemErr)
	}
}

// TestScanExternalListingPricesCancelsQuoteWait 验证服务关闭可立即中止下一次报价等待。
func TestScanExternalListingPricesCancelsQuoteWait(t *testing.T) {
	// store、cleanup 是隔离数据库和测试结束后的连接清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	createListingPriceSyncRules(t, store, 201, 202)
	// firstStarted 让测试在首个报价结束、第二个等待即将开始时取消扫描。
	firstStarted := make(chan struct{})
	// fulfillment 记录取消前实际到达供应站的报价次数。
	fulfillment := &listingPriceQuoteStub{firstStarted: firstStarted}
	// scheduler 使用足够长的测试间隔，确保取消发生在等待阶段。
	scheduler := NewScheduler(NewWithDependencies(store, nil, nil, CenterDependencies{ExternalFulfillment: fulfillment}))
	scheduler.externalListingQuoteInterval = time.Second
	// ctx、cancel 控制本轮扫描生命周期，done 通知扫描已经返回。
	ctx, cancel := context.WithCancel(context.Background())
	// done 由扫描 goroutine 关闭，供主测试等待取消收束。
	done := make(chan struct{})
	go func() { // scanRunner 在独立 goroutine 中模拟生产调度循环调用。
		defer close(done)
		scheduler.scanExternalListingPrices(ctx)
	}()
	select {
	case <-firstStarted:
		// settleDelay 让首个报价完成本地读价并进入第二个报价的长间隔等待。
		const settleDelay = 50 * time.Millisecond
		time.Sleep(settleDelay)
		cancel()
	case <-time.After(time.Second):
		t.Fatal("首个报价未开始")
	}
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("取消后价格同步仍阻塞在报价间隔等待")
	}
	// starts 是取消后实际发起的报价记录，第二个商品不得到达供应站。
	starts, _ := fulfillment.snapshot()
	if len(starts) != 1 {
		t.Fatalf("取消后仍发起后续报价: %d", len(starts))
	}
}
