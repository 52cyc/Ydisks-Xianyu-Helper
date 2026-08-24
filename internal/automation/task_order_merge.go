package automation

import "xianyu-go/internal/db"

// mergeOrderIntoTask 把本地订单已有事实补入自动化任务，不覆盖事件携带的新值。
func mergeOrderIntoTask(task Task, order *db.Order) Task {
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
	return task
}
