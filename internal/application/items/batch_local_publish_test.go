package items

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
)

// batchCompletionRepositoryFake 保存批次收口测试所需的状态和调用记录。
type batchCompletionRepositoryFake struct {
	// batch 保存当前用户可见的批次状态。
	batch BatchInfo
	// marked 保存成功检查点写入次数。
	marked int
	// markErr 模拟成功检查点写入失败。
	markErr error
	// getErr 模拟批次租约复核查询失败。
	getErr error
	// markSuccess 控制成功检查点是否匹配到当前租约；为空时默认成功。
	markSuccess *bool
}

// GetBatch 返回测试预置的批次状态。
func (repository *batchCompletionRepositoryFake) GetBatch(context.Context, int64, string) (BatchInfo, error) {
	if repository.getErr != nil {
		return BatchInfo{}, repository.getErr
	}
	return repository.batch, nil
}

// MarkClaimedRowSuccess 记录成功检查点写入并返回预置错误。
func (repository *batchCompletionRepositoryFake) MarkClaimedRowSuccess(_ context.Context, _ int64, _ string, _, _, _ string) (bool, error) {
	repository.marked++
	if repository.markErr != nil {
		return false, repository.markErr
	}
	if repository.markSuccess != nil {
		return *repository.markSuccess, nil
	}
	return true, nil
}

// batchPublishedItemRepositoryFake 保存本地商品写入测试结果。
type batchPublishedItemRepositoryFake struct {
	// item 保存最近一次写入的商品模型。
	item BatchPublishedItem
	// err 模拟本地商品写入失败。
	err error
}

// UpsertPublishedItem 记录商品写入并返回预置错误。
func (repository *batchPublishedItemRepositoryFake) UpsertPublishedItem(_ context.Context, item BatchPublishedItem) error {
	repository.item = item
	return repository.err
}

// batchPublishRuleRepositoryFake 保存自动化规则写入测试结果。
type batchPublishRuleRepositoryFake struct {
	// inputs 保存幂等规则写入请求。
	inputs []automationapp.RuleInput
	// sourceRules 保存克隆测试返回的源商品规则。
	sourceRules []automationapp.Rule
	// clonedSourceIDs 保存已请求幂等复制的源规则标识。
	clonedSourceIDs []int64
	// err 模拟规则写入失败。
	err error
}

// EnsurePublishRule 记录规则请求并返回预置错误。
func (repository *batchPublishRuleRepositoryFake) EnsurePublishRule(_ context.Context, input automationapp.RuleInput) error {
	repository.inputs = append(repository.inputs, input)
	return repository.err
}

// ListItemRules 返回预置的源商品规则快照。
func (repository *batchPublishRuleRepositoryFake) ListItemRules(_ context.Context, _ int64, _, _ string) ([]automationapp.Rule, error) {
	return append([]automationapp.Rule(nil), repository.sourceRules...), repository.err
}

// EnsureClonedPublishRule 记录克隆规则及其源规则标识。
func (repository *batchPublishRuleRepositoryFake) EnsureClonedPublishRule(_ context.Context, sourceRuleID int64, input automationapp.RuleInput) error {
	repository.clonedSourceIDs = append(repository.clonedSourceIDs, sourceRuleID)
	repository.inputs = append(repository.inputs, input)
	return repository.err
}

// TestBatchLocalPublishServiceCompletesLocalState 验证远端成功后的本地商品、规则和检查点收口顺序。
func TestBatchLocalPublishServiceCompletesLocalState(t *testing.T) {
	// completionRepository 保存租约状态和成功检查点记录。
	completionRepository := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}
	// itemRepository 保存本地商品写入结果。
	itemRepository := &batchPublishedItemRepositoryFake{}
	// ruleRepository 保存自动化规则写入结果。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 是待验证的批量本地收口服务。
	service, err := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if err != nil {
		t.Fatal(err)
	}
	// row 保存导入的商品和自动化配置。
	row := BatchRow{ID: 7, BatchID: "batch-1", CookieID: "cookie-1", Title: "导入标题", Description: "商品描述", Quantity: 2, AutomationJSON: `{"paid_delivery":{"enabled":true,"actions":[{"card_id":11,"delivery_count":2,"delay_seconds":3}]},"review_request":{"enabled":true,"after_shipped_hours":24,"message":"请评价","max_attempts":2,"delay_seconds":5}}`}
	// result 保存平台返回的非敏感商品结果。
	result := &BatchPublishResult{ItemID: "item-1", ItemURL: "https://example/item-1", Title: "平台标题", PriceText: "12.00", CategoryID: "cat-1", RawData: map[string]any{"ok": true}}
	// completeErr 保存本地收口结果。
	completeErr := service.Complete(context.Background(), 9, row, "worker", result)
	if completeErr != nil {
		t.Fatalf("本地收口失败: %v", completeErr)
	}
	if completionRepository.marked != 1 || itemRepository.item.ItemID != "item-1" || !itemRepository.item.MultiQuantityDelivery {
		t.Fatalf("本地商品或检查点状态异常: marked=%d item=%+v", completionRepository.marked, itemRepository.item)
	}
	if len(ruleRepository.inputs) != 2 || ruleRepository.inputs[0].Actions[1].ActionType != automationapp.ActionConfirmShipment || ruleRepository.inputs[1].TriggerType != automationapp.TriggerReviewMissingTimeout {
		t.Fatalf("自动化规则转换异常: %+v", ruleRepository.inputs)
	}
}

// TestBatchLocalPublishServiceCreatesExternalFulfillmentRule 验证发布成功后创建外部采购与确认发货动作。
func TestBatchLocalPublishServiceCreatesExternalFulfillmentRule(t *testing.T) {
	// completionRepository 保存当前 worker 的有效批次租约。
	completionRepository := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}
	// itemRepository 保存本地商品写入结果。
	itemRepository := &batchPublishedItemRepositoryFake{}
	// ruleRepository 捕获外部货源自动化规则。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 是待验证的批量本地收口服务。
	service, serviceErr := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// automationJSON 是选品批次持久化的外部货源配置。
	automationJSON := `{"external_delivery":{"enabled":true,"instance_id":3,"goods_id":9,"goods_name":"视频月卡","delivery_count":2,"safe_price":"5.60","profit_rate":"10.00","price_sync_enabled":true,"stop_purchase_on_inversion":true}}`
	// completeErr 是平台发布成功后的本地收口结果。
	completeErr := service.Complete(context.Background(), 1, BatchRow{ID: 7, BatchID: "batch-1", CookieID: "acc1", Title: "视频月卡", Quantity: 1, AutomationJSON: automationJSON}, "worker", &BatchPublishResult{ItemID: "item-9", Title: "视频月卡", PriceText: "3.08"})
	if completeErr != nil || len(ruleRepository.inputs) != 1 || len(ruleRepository.inputs[0].Actions) != 2 || ruleRepository.inputs[0].Actions[0].DeliveryCount != 2 || ruleRepository.inputs[0].Actions[1].ActionType != automationapp.ActionConfirmShipment {
		t.Fatalf("外部货源规则收口失败: rules=%+v err=%v", ruleRepository.inputs, completeErr)
	}
	// config 是发送卡密动作中供现有执行器消费的结构化配置。
	var config map[string]any
	if decodeErr := json.Unmarshal([]byte(ruleRepository.inputs[0].Actions[0].ConfigJSON), &config); decodeErr != nil { // decodeErr 是动作配置 JSON 解析错误。
		t.Fatal(decodeErr)
	}
	if config["source_type"] != "external" || config["goods_id"] != float64(9) || config["profit_rate"] != "10.00" {
		t.Fatalf("外部货源动作配置错误: %#v", config)
	}
	// ruleConfig 是批量创建的规则级买家通知配置。
	var ruleConfig map[string]any
	if decodeErr := json.Unmarshal([]byte(ruleRepository.inputs[0].ConfigJSON), &ruleConfig); decodeErr != nil { // decodeErr 是测试读取规则配置的错误。
		t.Fatal(decodeErr)
	}
	if ruleConfig["price_guidance_enabled"] != true || ruleConfig["price_adjusted_notice_enabled"] != true {
		t.Fatalf("批量货源规则未默认开启买家通知: %#v", ruleConfig)
	}
}

// TestBatchLocalPublishServiceClonesAllItemRules 验证账号间克隆会复制源商品的全部规则、动作和开关。
func TestBatchLocalPublishServiceClonesAllItemRules(t *testing.T) {
	// enabled 表示源动作的启用状态仅用于构造快照。
	enabled := true
	// ruleRepository 保存两条具有不同触发类型的源商品规则。
	ruleRepository := &batchPublishRuleRepositoryFake{sourceRules: []automationapp.Rule{
		{ID: 11, Name: "付款后自动发货", TriggerType: automationapp.TriggerOrderPaid, Enabled: true, Priority: 20, ConfigJSON: `{"notice":true}`, SKUMigrationStatus: "ready", Actions: []automationapp.Action{{ID: 101, ActionType: automationapp.ActionSendCard, CardID: 7, DeliveryCount: 2, Enabled: enabled, SortOrder: 1}}},
		{ID: 12, Name: "评价后赠品", TriggerType: automationapp.TriggerBuyerReviewed, Enabled: false, Priority: 30, ConfigJSON: `{}`, Actions: []automationapp.Action{{ID: 102, ActionType: automationapp.ActionSendText, MessageTemplate: "谢谢", Enabled: enabled, SortOrder: 1}}},
	}}
	// service 是具有有效租约和规则克隆端口的本地收口服务。
	service, serviceErr := NewBatchLocalPublishService(&batchCompletionRepositoryFake{}, &batchPublishedItemRepositoryFake{}, ruleRepository)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// cloneErr 是克隆配置转换为目标商品规则的结果。
	cloneErr := service.EnsureAutomationRules(context.Background(), 9, BatchRow{CookieID: "target-account", AutomationJSON: `{"clone_source":{"cookie_id":"source-account","item_id":"source-item"}}`}, &BatchPublishResult{ItemID: "target-item"})
	if cloneErr != nil || len(ruleRepository.inputs) != 2 || len(ruleRepository.clonedSourceIDs) != 2 {
		t.Fatalf("克隆规则数量异常: err=%v inputs=%+v source_ids=%v", cloneErr, ruleRepository.inputs, ruleRepository.clonedSourceIDs)
	}
	// first、second 分别是目标商品的付款和评价规则。
	first, second := ruleRepository.inputs[0], ruleRepository.inputs[1]
	if first.CookieID != "target-account" || first.ItemID != "target-item" || first.Actions[0].ID != 0 || first.Actions[0].CardID != 7 || !strings.Contains(first.ConfigJSON, `"clone_source_rule_id":11`) {
		t.Fatalf("首条克隆规则不完整: %+v", first)
	}
	if second.Enabled || second.Priority != 30 || second.Actions[0].MessageTemplate != "谢谢" || ruleRepository.clonedSourceIDs[1] != 12 {
		t.Fatalf("第二条克隆规则未保留开关与动作: %+v", second)
	}
}

// TestBatchLocalPublishServiceRejectsLeaseLoss 验证租约丢失时不写入本地商品或成功检查点。
func TestBatchLocalPublishServiceRejectsLeaseLoss(t *testing.T) {
	// completionRepository 返回已转移到其他 worker 的批次租约。
	completionRepository := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "other"}}
	// itemRepository 保存本地商品写入调用次数的替身。
	itemRepository := &batchPublishedItemRepositoryFake{}
	// ruleRepository 保存规则写入调用次数的替身。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 是待验证的批量本地收口服务。
	service, err := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if err != nil {
		t.Fatal(err)
	}
	// completeErr 保存租约丢失后的错误。
	completeErr := service.Complete(context.Background(), 9, BatchRow{BatchID: "batch-1", AutomationJSON: `{}`}, "worker", &BatchPublishResult{ItemID: "item-1"})
	if !errors.Is(completeErr, context.Canceled) || itemRepository.item.ItemID != "" || completionRepository.marked != 0 {
		t.Fatalf("租约丢失处理异常: err=%v item=%+v marked=%d", completeErr, itemRepository.item, completionRepository.marked)
	}
}

// TestBatchLocalPublishServiceClassifiesPostWriteFailure 验证商品或规则写入失败会标记为不可自动重试的后置错误。
func TestBatchLocalPublishServiceClassifiesPostWriteFailure(t *testing.T) {
	// completionRepository 保存可用的当前 worker 租约。
	completionRepository := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}
	// itemRepository 模拟数据库商品写入故障。
	itemRepository := &batchPublishedItemRepositoryFake{err: errors.New("商品数据库不可用")}
	// ruleRepository 保存未被调用的规则写入替身。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 是待验证的批量本地收口服务。
	service, err := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if err != nil {
		t.Fatal(err)
	}
	// completeErr 保存本地写入失败后的错误。
	completeErr := service.Complete(context.Background(), 9, BatchRow{BatchID: "batch-1", AutomationJSON: `{}`}, "worker", &BatchPublishResult{ItemID: "item-1"})
	// postErr 保存后置本地收口错误的类型化视图。
	var postErr *PostPublishError
	if !errors.As(completeErr, &postErr) || completionRepository.marked != 0 || len(ruleRepository.inputs) != 0 {
		t.Fatalf("后置错误分类异常: err=%v marked=%d rules=%d", completeErr, completionRepository.marked, len(ruleRepository.inputs))
	}
}
