package automation

import (
	"fmt"
	"strings"
)

// buildTriggerKey 为订单事件建立稳定的自动化防重键。
func buildTriggerKey(task Task) string {
	if task.Raw != nil {
		// marker、ok 保存未知角色事件的原始防重键及存在性，订单补齐后仍沿用原键。
		if marker, ok := task.Raw[roleVerificationTaskKeyField].(string); ok && strings.TrimSpace(marker) != "" {
			return strings.TrimSpace(marker)
		}
	}
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		// attempt、ok 保存求评价重试序号及其存在性，避免不同轮次共享同一防重键。
		if attempt, ok := task.Raw["attempt"]; ok {
			return fmt.Sprintf("%s:%s:%v", task.TriggerType, task.OrderID, attempt)
		}
	}
	if task.OrderID != "" {
		return task.TriggerType + ":" + task.OrderID
	}
	if task.UpdateKey != "" {
		return task.TriggerType + ":" + task.UpdateKey
	}
	return ""
}
