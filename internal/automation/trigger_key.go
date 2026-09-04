package automation

import "fmt"

// buildTriggerKey 为订单事件建立稳定的自动化防重键。
func buildTriggerKey(task Task) string {
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
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
