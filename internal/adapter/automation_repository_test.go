package adapter

import (
	"context"
	"errors"
	"strconv"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/db"
)

// TestAutomationRepositoryOwnershipMapsNotFound 验证资源缺失只表现为不归属，不泄露数据库模型错误。
func TestAutomationRepositoryOwnershipMapsNotFound(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// owned、ownedErr 保存不存在商品的归属判断结果。
	owned, ownedErr := repository.OwnsItem(ctx, 1, "cid", "missing-item")
	if owned || ownedErr != nil {
		t.Fatalf("缺失商品应返回 false,nil，owned=%v err=%v", owned, ownedErr)
	}
	// cardInfo、cardErr 保存不存在卡密组的最小应用摘要及错误。
	cardInfo, cardErr := repository.GetCard(ctx, 1, 999999)
	if cardInfo != (automationapp.CardInfo{}) || !errors.Is(cardErr, automationapp.ErrRuleNotFound) {
		t.Fatalf("缺失卡密组应映射为应用未找到，info=%+v err=%v", cardInfo, cardErr)
	}
}

// TestAutomationRepositoryOwnershipPropagatesDatabaseErrors 验证数据库不可用时不会伪装成归属失败。
func TestAutomationRepositoryOwnershipPropagatesDatabaseErrors(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// ctx 是数据库关闭后调用适配器的非取消上下文。
	ctx := context.Background()
	// closeErr 表示提前关闭测试数据库时的资源释放错误。
	closeErr := store.DB.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// _, itemErr 保存商品归属查询透传的数据库错误。
	_, itemErr := repository.OwnsItem(ctx, 1, "cid", "item-1")
	if itemErr == nil {
		t.Fatal("数据库关闭后商品归属查询应返回错误")
	}
	// _, cardErr 保存卡密组查询透传的数据库错误。
	_, cardErr := repository.GetCard(ctx, 1, 1)
	if cardErr == nil || errors.Is(cardErr, db.ErrNotFound) || errors.Is(cardErr, automationapp.ErrRuleNotFound) {
		t.Fatalf("数据库关闭后卡密查询应保留底层错误，err=%v", cardErr)
	}
}

// TestAutomationRepositoryEnsurePublishRuleIsIdempotent 验证发布自动化规则只创建一次。
func TestAutomationRepositoryEnsurePublishRuleIsIdempotent(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// input 是代表发布成功后自动发货的规则输入。
	input := automationapp.RuleInput{UserID: 1, CookieID: "cid", ItemID: "item-1", Name: "付款后自动发货 - 商品", TriggerType: automationapp.TriggerOrderPaid, Enabled: true, Priority: 100, ConfigJSON: "{}", Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionConfirmShipment, Enabled: true, SortOrder: 1}}}
	// firstErr 保存第一次幂等准备的错误。
	firstErr := repository.EnsurePublishRule(ctx, input)
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	// secondErr 保存第二次幂等准备的错误。
	secondErr := repository.EnsurePublishRule(ctx, input)
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	// rules、matchErr 保存按发布规则唯一条件查询到的规则。
	rules, matchErr := store.Automation.Match(ctx, input.CookieID, input.ItemID, input.TriggerType)
	if matchErr != nil || len(rules) != 1 {
		t.Fatalf("幂等准备应只保留一条规则，rules=%+v err=%v", rules, matchErr)
	}
}

// TestAutomationRepositoryClonesExactItemRulesIdempotently 验证克隆只读取源商品规则，且同一源规则恢复时不重复创建。
func TestAutomationRepositoryClonesExactItemRulesIdempotently(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// owner、ownerErr 保存测试用户及读取错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	for /* accountID 表示当前待建立归属关系的源或目标账号。 */ _, accountID := range []string{"source-account", "target-account"} {
		if saveErr := store.Cookies.Save(ctx, accountID, "cookie", owner.ID); saveErr != nil {
			t.Fatal(saveErr)
		}
	}
	// sourceRuleID、sourceErr 保存源商品规则标识及写入错误。
	sourceRuleID, sourceErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: owner.ID, CookieID: "source-account", ItemID: "source-item", Name: "源商品规则", TriggerType: automationapp.TriggerOrderPaid, Enabled: true, Priority: 20, ConfigJSON: `{}`, Actions: []db.AutomationActionInput{{ActionType: automationapp.ActionConfirmShipment, Enabled: true, SortOrder: 1}}})
	if sourceErr != nil {
		t.Fatal(sourceErr)
	}
	// accountRuleID、accountRuleErr 保存不应被商品克隆的账号通用规则写入结果。
	accountRuleID, accountRuleErr := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: owner.ID, CookieID: "source-account", ItemID: "", Name: "账号通用规则", TriggerType: automationapp.TriggerBuyerReviewed, Enabled: true, Priority: 30, ConfigJSON: `{}`, Actions: []db.AutomationActionInput{{ActionType: automationapp.ActionSendText, MessageTemplate: "谢谢", Enabled: true, SortOrder: 1}}})
	if accountRuleErr != nil || accountRuleID <= 0 {
		t.Fatalf("准备账号通用规则失败: id=%d err=%v", accountRuleID, accountRuleErr)
	}
	// rules、listErr 保存按源商品精确筛选的规则快照。
	rules, listErr := repository.ListItemRules(ctx, owner.ID, "source-account", "source-item")
	if listErr != nil || len(rules) != 1 || rules[0].ID != sourceRuleID {
		t.Fatalf("源商品规则筛选异常: rules=%+v err=%v", rules, listErr)
	}
	// cloneInput 是携带源规则标识的目标商品规则。
	cloneInput := automationapp.RuleInput{UserID: owner.ID, CookieID: "target-account", ItemID: "target-item", Name: rules[0].Name, TriggerType: rules[0].TriggerType, Enabled: true, Priority: rules[0].Priority, ConfigJSON: `{"clone_source_rule_id":` + strconv.FormatInt(sourceRuleID, 10) + `}`, Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionConfirmShipment, Enabled: true, SortOrder: 1}}}
	// firstErr 保存首次克隆目标规则的写入结果。
	firstErr := repository.EnsureClonedPublishRule(ctx, sourceRuleID, cloneInput)
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	// secondErr 保存重复克隆同一源规则的幂等结果。
	secondErr := repository.EnsureClonedPublishRule(ctx, sourceRuleID, cloneInput)
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	// targetRules、targetErr 保存目标商品最终的规则集合。
	targetRules, targetErr := repository.ListItemRules(ctx, owner.ID, "target-account", "target-item")
	if targetErr != nil || len(targetRules) != 1 {
		t.Fatalf("克隆规则幂等性异常: rules=%+v err=%v", targetRules, targetErr)
	}
}

// TestAutomationRepositoryEnsurePublishRulePropagatesDatabaseError 验证数据库故障不会被发布规则适配器吞掉。
func TestAutomationRepositoryEnsurePublishRulePropagatesDatabaseError(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定 SQLite 存储的自动化规则适配器。
	repository := NewAutomationRepository(store)
	// closeErr 表示提前关闭测试数据库时的资源释放错误。
	closeErr := store.DB.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// err 保存关闭数据库后执行幂等准备的错误。
	err := repository.EnsurePublishRule(context.Background(), automationapp.RuleInput{UserID: 1, CookieID: "account-1", ItemID: "item-1", Name: "规则", TriggerType: automationapp.TriggerOrderPaid})
	if err == nil {
		t.Fatal("数据库关闭后应返回发布规则持久化错误")
	}
}
