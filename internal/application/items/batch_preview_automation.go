package items

// parseAutomation 解析表格中的自动化配置文本。
func parseAutomation(fields map[string]any) BatchPreviewAutomation {
	// paidActions 和 paidError 表示付款发货动作及解析错误。
	paidActions, paidError := parseCardActions(firstString(fields, "paid_delivery_contents", "付款发货内容"))
	// reviewActions 和 reviewError 表示评价赠品动作及解析错误。
	reviewActions, reviewError := parseCardActions(firstString(fields, "review_gift_contents", "评价赠品内容"))
	// cloneSourceAccountID、cloneSourceItemID 是账号间克隆快照携带的源商品双键。
	cloneSourceAccountID, cloneSourceItemID := firstString(fields, "clone_source_account_id"), firstString(fields, "clone_source_item_id")
	// cloneSource 只在任一源定位字段存在时创建，便于预检报告成对约束错误。
	var cloneSource *BatchPreviewCloneSource
	if cloneSourceAccountID != "" || cloneSourceItemID != "" {
		cloneSource = &BatchPreviewCloneSource{CookieID: cloneSourceAccountID, ItemID: cloneSourceItemID}
	}
	return BatchPreviewAutomation{
		PaidDelivery:  BatchPreviewCardAutomation{Enabled: parseBool(firstString(fields, "paid_delivery_enabled", "付款发货启用")), Actions: paidActions, ParseError: paidError},
		ReviewGift:    BatchPreviewCardAutomation{Enabled: parseBool(firstString(fields, "review_gift_enabled", "评价赠品启用")), Actions: reviewActions, ParseError: reviewError},
		ReviewRequest: BatchPreviewReviewRequest{Enabled: parseBool(firstString(fields, "review_request_enabled", "求评价启用")), AfterShippedHours: parseIntDefault(firstString(fields, "review_request_after_hours", "求评价等待小时"), 72), Message: firstString(fields, "review_request_message", "求评价文案"), MaxAttempts: parseIntDefault(firstString(fields, "review_request_max_attempts", "求评价最多次数"), 1), DelaySeconds: parseIntDefault(firstString(fields, "review_request_delay_seconds", "求评价延迟秒"), 0)},
		ExternalDelivery: BatchPreviewExternalDelivery{
			Enabled: parseBool(firstString(fields, "external_delivery_enabled")), InstanceID: int64(parseIntDefault(firstString(fields, "external_instance_id"), 0)),
			GoodsID: int64(parseIntDefault(firstString(fields, "external_goods_id"), 0)), GoodsName: firstString(fields, "external_goods_name"),
			GoodsType: parseIntDefault(firstString(fields, "external_goods_type"), 0), DeliveryCount: parseIntDefault(firstString(fields, "external_delivery_count"), 1), SafePrice: firstString(fields, "external_safe_price"),
			ProfitRate: firstString(fields, "external_profit_rate"), PriceSyncEnabled: parseBool(firstString(fields, "external_price_sync_enabled")),
			StopPurchaseOnInversion: parseBool(firstString(fields, "external_stop_purchase_on_inversion")),
		},
		CloneSource: cloneSource,
	}
}
