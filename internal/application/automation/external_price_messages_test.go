package automation

import (
	"strings"
	"testing"
)

// TestValidateExternalPriceMessageConfig 验证咨询引导只能绑定商品级外部跟价规则，并限制超长聊天文案。
func TestValidateExternalPriceMessageConfig(t *testing.T) {
	// enabled 是同时开启首次引导和最终价格通知的有效规则配置。
	enabled := `{"price_guidance_enabled":true,"price_guidance_text":"请先拍下不要付款","price_adjusted_notice_enabled":true,"price_adjusted_notice_text":"最终价格 {price}"}`
	if validateErr := validateExternalPriceMessageConfig(enabled, TriggerOrderPaid, "item-1", true, true); validateErr != nil { // validateErr 是有效配置不应产生的校验错误。
		t.Fatalf("有效跟价消息配置被拒绝: %v", validateErr)
	}
	// failureNotice 是不依赖动态改价、只要求外部货源动作的有效最终失败通知。
	failureNotice := `{"fulfillment_failure_notice_enabled":true,"fulfillment_failure_notice_text":"正在人工核实 {order_id}"}`
	if validateErr := validateExternalPriceMessageConfig(failureNotice, TriggerOrderPaid, "item-1", false, true); validateErr != nil { // validateErr 是直接付款外部采购失败通知不应产生的校验错误。
		t.Fatalf("有效采购失败通知被拒绝: %v", validateErr)
	}
	// cases 保存应被拒绝的触发范围、商品范围、动态跟价状态和文案长度组合。
	cases := []struct {
		// name 是测试子场景名称。
		name string
		// raw、triggerType、itemID、dynamic 和 external 分别是规则 JSON、触发类型、商品范围、动态跟价及外部货源开关。
		raw         string
		triggerType string
		itemID      string
		dynamic     bool
		external    bool
	}{
		{name: "未开启动态跟价", raw: enabled, triggerType: TriggerOrderPaid, itemID: "item-1", dynamic: false, external: true},
		{name: "没有具体商品", raw: enabled, triggerType: TriggerOrderPaid, itemID: "", dynamic: true, external: true},
		{name: "错误触发类型", raw: enabled, triggerType: TriggerOrderCreated, itemID: "item-1", dynamic: true, external: true},
		{name: "文案过长", raw: `{"price_guidance_enabled":true,"price_guidance_text":"` + strings.Repeat("长", 1001) + `"}`, triggerType: TriggerOrderPaid, itemID: "item-1", dynamic: true, external: true},
		{name: "查询提示过长", raw: `{"price_guidance_enabled":true,"price_query_prompt_text":"` + strings.Repeat("长", 1001) + `"}`, triggerType: TriggerOrderPaid, itemID: "item-1", dynamic: true, external: true},
		{name: "失败通知没有外部货源", raw: failureNotice, triggerType: TriggerOrderPaid, itemID: "item-1", dynamic: false, external: false},
		{name: "失败通知文案过长", raw: `{"fulfillment_failure_notice_enabled":true,"fulfillment_failure_notice_text":"` + strings.Repeat("长", 1001) + `"}`, triggerType: TriggerOrderPaid, itemID: "item-1", dynamic: false, external: true},
		{name: "保护价提示文案过长", raw: `{"fulfillment_failure_notice_enabled":true,"fulfillment_safe_price_notice_text":"` + strings.Repeat("长", 1001) + `"}`, triggerType: TriggerOrderPaid, itemID: "item-1", dynamic: false, external: true},
	}
	for _, testCase := range cases { // testCase 是当前应被拒绝的规则组合。
		t.Run(testCase.name, func(t *testing.T) { // t 是隔离当前校验分支的测试句柄。
			if validateErr := validateExternalPriceMessageConfig(testCase.raw, testCase.triggerType, testCase.itemID, testCase.dynamic, testCase.external); validateErr == nil { // validateErr 应说明当前配置为何不可保存。
				t.Fatal("无效跟价消息配置未被拒绝")
			}
		})
	}
}
