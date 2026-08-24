package automation

import (
	"context"

	"xianyu-go/internal/xianyu/mtop"
)

// CenterDependencies 保存自动化中心启动时必须固定的外部协作依赖。
type CenterDependencies struct {
	// MTop 提供确认发货使用的 MTOP 协议客户端；为空时使用默认实现。
	MTop mtop.Client
	// AccountTaskClient 提供自动评价与商品擦亮的协议调用；使用默认 MTOP 时可自动复用其任务能力。
	AccountTaskClient AccountTaskClient
	// OrderDetailFetcher 提供自动发货前的订单详情查询能力。
	OrderDetailFetcher OrderDetailFetcher
	// Notifier 接收发货结果通知；为空时不发送通知。
	Notifier Notifier
	// CookieSource 提供自动发货读取 Cookie 的可替换边界；为空时读取仓储。
	CookieSource func(context.Context, string) (string, error)
	// APICardFetcher 提供普通 API 卡发货请求能力；为空时 API 卡执行会明确失败。
	APICardFetcher APICardFetcher
	// ExternalFulfillment 提供外部货源的幂等采购与查单能力。
	ExternalFulfillment ExternalFulfillment
}

// ExternalFulfillmentRequest 是自动化执行器发起的外部采购请求。
type ExternalFulfillmentRequest struct {
	// UserID 是闲鱼账号所属用户。
	UserID int64
	// InstanceID 是规则选择的货源实例。
	InstanceID int64
	// ExternalOrderNo 是由闲鱼订单和动作生成的稳定幂等键。
	ExternalOrderNo string
	// XianyuOrderID 是原始闲鱼订单号。
	XianyuOrderID string
	// GoodsID 是货源站商品主键。
	GoodsID int64
	// Quantity 是本次采购数量。
	Quantity int
	// SafePrice 是管理员设定的采购保护价。
	SafePrice string
	// Attach 是直充商品要求的附加字段。
	Attach map[string]string
}

// ExternalFulfillmentResult 是外部采购的已落库结果。
type ExternalFulfillmentResult struct {
	// State 是本地统一履约状态。
	State string
	// Cards 是可发送给买家的卡密列表。
	Cards []string
	// RechargeInfo 是直充成功结果。
	RechargeInfo string
	// RechargeHints 是直充进度或提示。
	RechargeHints string
}

// ExternalFulfillment 屏蔽卡速售应用模型，只向自动化暴露幂等履约结果。
type ExternalFulfillment interface {
	Fulfill(context.Context, ExternalFulfillmentRequest) (ExternalFulfillmentResult, error)
}
