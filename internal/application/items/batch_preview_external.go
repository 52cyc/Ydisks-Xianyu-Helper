package items

// BatchPreviewExternalDelivery 是货源选品批量发布使用的单商品自动发货配置。
type BatchPreviewExternalDelivery struct {
	// Enabled 表示发布成功后创建外部货源付款发货规则。
	Enabled bool `json:"enabled"`
	// InstanceID 是当前用户选择的货源实例主键。
	InstanceID int64 `json:"instance_id"`
	// GoodsID 是一个单规格货源商品标识。
	GoodsID int64 `json:"goods_id"`
	// GoodsName 是规则中用于管理员识别的货源商品名称。
	GoodsName string `json:"goods_name"`
	// GoodsType 是货源商品交付类型；首版仅允许返回卡密的类型一。
	GoodsType int `json:"goods_type"`
	// DeliveryCount 是每卖出一件闲鱼商品需要采购的供应商商品份数。
	DeliveryCount int `json:"delivery_count"`
	// SafePrice 是发布前读取的采购单价，供直接付款倒挂保护使用。
	SafePrice string `json:"safe_price"`
	// ProfitRate 是售价相对采购价的加价百分比。
	ProfitRate string `json:"profit_rate"`
	// PriceSyncEnabled 表示后续货源价格变化时同步闲鱼售价。
	PriceSyncEnabled bool `json:"price_sync_enabled"`
	// StopPurchaseOnInversion 表示实时采购成本高于买家实付时停止采购。
	StopPurchaseOnInversion bool `json:"stop_purchase_on_inversion"`
}
