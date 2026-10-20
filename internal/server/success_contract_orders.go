package server

// orderRefreshDetailResponse 是订单刷新后返回的远端详情 DTO。
type orderRefreshDetailResponse struct {
	// Quantity 是订单购买数量。
	Quantity string `json:"quantity"`
	// SpecName 是商品规格名称。
	SpecName string `json:"spec_name"`
	// SpecValue 是商品规格值。
	SpecValue string `json:"spec_value"`
	// OrderStatus 是归一化后的订单状态。
	OrderStatus string `json:"order_status"`
	// Amount 是订单实付金额。
	Amount string `json:"amount"`
}

// orderRefreshResponse 是订单列表刷新接口的具名响应 DTO。
type orderRefreshResponse struct {
	// PartialFailure 表示批量刷新是否存在部分失败。
	PartialFailure bool `json:"partial_failure"`
	// Message 是刷新结果说明。
	Message string `json:"message"`
	// Summary 是刷新统计摘要。
	Summary orderRefreshSummary `json:"summary"`
	// Results 是逐账号或逐订单的兼容结果行。
	Results []orderRefreshResultDTO `json:"results"`
}

// orderRefreshResultDTO 是订单刷新批次中单条结果的稳定响应 DTO；可选字段只在对应阶段产生时返回。
type orderRefreshResultDTO struct {
	// Success 表示当前账号或订单刷新是否成功。
	Success bool `json:"success"`
	// CookieID 是当前结果关联的账号标识。
	CookieID string `json:"cookie_id,omitempty"`
	// Discovered 是当前账号发现的新订单数量。
	Discovered *int `json:"discovered,omitempty"`
	// Updated 是当前账号更新的订单数量。
	Updated *int `json:"updated,omitempty"`
	// SoftDeleted 表示当前账号是否标记了失效订单。
	SoftDeleted *bool `json:"soft_deleted,omitempty"`
	// OrderID 是单订单刷新结果关联的平台订单标识。
	OrderID string `json:"order_id,omitempty"`
	// Stage 是刷新失败或完成所处的业务阶段。
	Stage string `json:"stage,omitempty"`
	// Message 是面向客户端的结果说明。
	Message string `json:"message,omitempty"`
	// Error 是当前结果的兼容错误文本。
	Error string `json:"error,omitempty"`
	// OldStatus 是刷新前的订单状态。
	OldStatus string `json:"old_status,omitempty"`
	// NewStatus 是刷新后的订单状态。
	NewStatus string `json:"new_status,omitempty"`
}

// orderRefreshSummary 是订单列表刷新统计摘要 DTO。
type orderRefreshSummary struct {
	// Restored 是同账号软删除订单恢复数，与新增和历史错绑修正不重复计数；零值可省略以兼容旧任务。
	Restored int `json:"restored,omitempty"`
	// Reassigned 是经身份核验修正历史错绑的订单数；零值可省略以兼容旧任务。
	Reassigned int `json:"reassigned,omitempty"`
	// Discovered 是发现的新订单数量。
	Discovered int `json:"discovered"`
	// ListUpdated 是订单列表更新数量。
	ListUpdated int `json:"list_updated"`
	// SoftDeleted 是标记删除的订单数量。
	SoftDeleted int `json:"soft_deleted"`
	// DetailTotal 是兼容旧任务契约的详情补全数量；批量列表同步固定为零。
	DetailTotal int `json:"detail_total"`
	// Total 是兼容旧任务契约的详情处理总数；批量列表同步固定为零。
	Total int `json:"total"`
	// Updated 是兼容旧任务契约的详情刷新变化数量；批量列表字段变化计入 ListUpdated。
	Updated int `json:"updated"`
	// NoChange 是兼容旧任务契约的详情刷新未变化数量；批量列表同步固定为零。
	NoChange int `json:"no_change"`
	// Failed 是刷新失败数量。
	Failed int `json:"failed"`
}

// orderSingleRefreshResponse 是单订单刷新接口的具名响应 DTO。
type orderSingleRefreshResponse struct {
	// Success 表示刷新是否完成。
	Success bool `json:"success"`
	// Message 是刷新结果说明。
	Message string `json:"message"`
	// Order 是刷新后的订单详情。
	Order orderRefreshDetailResponse `json:"order"`
}

// manualShipResponse 是手动发货接口的具名响应 DTO。
type manualShipResponse struct {
	// PartialFailure 表示批量发货是否存在部分失败。
	PartialFailure bool `json:"partial_failure"`
	// Message 是发货结果说明。
	Message string `json:"message"`
	// SuccessCount 是成功发货数量。
	SuccessCount int `json:"success_count"`
	// FailedCount 是失败发货数量。
	FailedCount int `json:"failed_count"`
	// Results 是逐订单的兼容结果行。
	Results []orderMutationResultDTO `json:"results"`
}

// orderMutationResultDTO 是手动发货逐订单结果的稳定响应 DTO。
type orderMutationResultDTO struct {
	// OrderID 是当前结果关联的平台订单标识。
	OrderID string `json:"order_id"`
	// Status 是发货后订单状态或失败阶段。
	Status string `json:"status"`
	// Success 表示当前订单是否发货成功。
	Success bool `json:"success"`
	// Message 是当前订单的处理说明。
	Message string `json:"message"`
	// ReconciliationID 是可选的补偿记录标识。
	ReconciliationID string `json:"reconciliation_id,omitempty"`
	// ReconciliationWarning 是可选的补偿告警文本。
	ReconciliationWarning string `json:"reconciliation_warning,omitempty"`
}

// orderImportResultDTO 是订单导入逐订单结果的稳定响应 DTO。
type orderImportResultDTO struct {
	// OrderID 是当前结果关联的平台订单标识。
	OrderID string `json:"order_id"`
	// Success 表示当前订单是否导入成功。
	Success bool `json:"success"`
	// Message 是当前订单的处理说明。
	Message string `json:"message"`
}
