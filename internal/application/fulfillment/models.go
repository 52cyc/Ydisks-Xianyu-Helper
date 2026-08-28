// Package fulfillment 定义外部货源履约的应用用例和窄端口。
package fulfillment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

const (
	// ProviderKasushouV2 表示兼容卡速售 v2 协议的货源实例。
	ProviderKasushouV2 = "kasushou_v2"
	// ProviderKayixinV3 表示使用卡易信商家客户 API 3.0 协议的货源实例。
	ProviderKayixinV3 = "kayixin_v3"
	// ProviderMifengV1 表示使用蜜蜂汇云商户采购 API 的货源实例。
	ProviderMifengV1 = "mifeng_v1"
	// GoodsTypeCard 表示履约成功后返回卡密。
	GoodsTypeCard = 1
	// GoodsTypeRecharge 表示需要提交账号、手机号等附加字段的直充商品。
	GoodsTypeRecharge = 2
)

var (
	// ErrNotFound 表示当前用户名下不存在指定资源。
	ErrNotFound = errors.New("外部履约资源不存在")
	// ErrConflict 表示幂等键、名称或映射冲突。
	ErrConflict = errors.New("外部履约资源冲突")
	// ErrSafePriceExceeded 表示供应站明确因为实时采购价超过保护价而拒绝下单。
	ErrSafePriceExceeded = errors.New("外部货源实时价格超过保护价")
	// ErrSubmissionUncertain 表示请求可能已到达供应站，只能使用原外部单号查单。
	ErrSubmissionUncertain = errors.New("外部货源下单结果未确认")
)

// IsSafePriceExceededMessage 判断供应站业务文案是否明确指向保护价拦截，不把普通网络或系统异常误判为涨价。
func IsSafePriceExceededMessage(message string) bool {
	// normalized 是去除空白并统一小写后的供应站业务提示。
	normalized := strings.ToLower(strings.Join(strings.Fields(message), ""))
	return strings.Contains(normalized, "保护价") || strings.Contains(normalized, "安全价") ||
		strings.Contains(normalized, "safeprice") || strings.Contains(normalized, "safe_price")
}

// Capabilities 记录一个外部货源实例实际支持的可选能力。
type Capabilities struct {
	// OrderList 表示站点开放订单列表接口。
	OrderList bool `json:"order_list"`
	// OrderCallback 表示站点会回调订单结果。
	OrderCallback bool `json:"order_callback"`
	// CancelCallback 表示站点会回调取消结果。
	CancelCallback bool `json:"cancel_callback"`
	// CancelRequestMode 指定取消接口使用 callback_url、card_list 或 ordersn_only。
	CancelRequestMode string `json:"cancel_request_mode"`
	// CardShowType 表示站点响应中可能携带卡密展示类型。
	CardShowType bool `json:"card_show_type"`
}

// Instance 是一个可独立配置并按 provider 路由的外部货源站。
type Instance struct {
	ID             int64        `json:"id"`
	PublicID       string       `json:"public_id"`
	UserID         int64        `json:"-"`
	Name           string       `json:"name"`
	Provider       string       `json:"provider"`
	BaseURL        string       `json:"base_url"`
	MerchantUserID string       `json:"merchant_user_id"`
	APIKey         string       `json:"-"`
	HasAPIKey      bool         `json:"has_api_key"`
	Capabilities   Capabilities `json:"capabilities"`
	Enabled        bool         `json:"enabled"`
	CreatedAt      string       `json:"created_at"`
	UpdatedAt      string       `json:"updated_at"`
}

// InstanceInput 是创建或更新货源实例的可写字段。
type InstanceInput struct {
	Name           string       `json:"name"`
	Provider       string       `json:"provider"`
	BaseURL        string       `json:"base_url"`
	MerchantUserID string       `json:"merchant_user_id"`
	APIKey         string       `json:"api_key"`
	Capabilities   Capabilities `json:"capabilities"`
	Enabled        bool         `json:"enabled"`
}

// Mapping 把闲鱼商品规格映射到远程货源商品。
type Mapping struct {
	ID            int64             `json:"id"`
	UserID        int64             `json:"-"`
	AccountID     string            `json:"account_id"`
	ItemID        string            `json:"item_id"`
	SpecName      string            `json:"spec_name"`
	SpecValue     string            `json:"spec_value"`
	InstanceID    int64             `json:"instance_id"`
	RemoteGoodsID int64             `json:"remote_goods_id"`
	GoodsType     int               `json:"goods_type"`
	SafePrice     string            `json:"safe_price"`
	QuantityMode  string            `json:"quantity_mode"`
	FixedQuantity int               `json:"fixed_quantity"`
	AttachMapping map[string]string `json:"attach_mapping"`
	Enabled       bool              `json:"enabled"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
}

// MappingInput 是商品映射的可写字段。
type MappingInput struct {
	AccountID     string            `json:"account_id"`
	ItemID        string            `json:"item_id"`
	SpecName      string            `json:"spec_name"`
	SpecValue     string            `json:"spec_value"`
	InstanceID    int64             `json:"instance_id"`
	RemoteGoodsID int64             `json:"remote_goods_id"`
	GoodsType     int               `json:"goods_type"`
	SafePrice     string            `json:"safe_price"`
	QuantityMode  string            `json:"quantity_mode"`
	FixedQuantity int               `json:"fixed_quantity"`
	AttachMapping map[string]string `json:"attach_mapping"`
	Enabled       bool              `json:"enabled"`
}

// Product 是不同货源协议归一化后的商品摘要。
type Product struct {
	ID        int64         `json:"id"`
	Name      string        `json:"goods_name"`
	Image     string        `json:"goods_img"`
	GoodsType int           `json:"goods_type"`
	FaceValue string        `json:"face_value"`
	Price     string        `json:"goods_price"`
	Status    int           `json:"status"`
	Stock     int           `json:"stock_num"`
	CanBuy    bool          `json:"can_buy"`
	Attach    []AttachField `json:"attach,omitempty"`
}

// AttachField 描述直充商品下单时要求的一个动态字段。
type AttachField struct {
	Type       string     `json:"type"`
	Name       string     `json:"name"`
	Key        string     `json:"key"`
	Validation string     `json:"vali"`
	Tip        string     `json:"tip"`
	Options    StringList `json:"options,omitempty"`
}

// StringList 兼容卡速售兼容站把选项返回为数组、JSON 字符串或普通分隔字符串。
type StringList []string

// UnmarshalJSON 把卡速售 options 的字符串或数组响应统一转换为字符串数组。
func (options *StringList) UnmarshalJSON(data []byte) error {
	// direct 是官方或兼容站直接返回的字符串数组。
	var direct []string
	if err /* err 是数组形式的解析结果。 */ := json.Unmarshal(data, &direct); err == nil {
		*options = StringList(direct)
		return nil
	}
	// raw 是智客等兼容站返回的空字符串、JSON 字符串或分隔字符串。
	var raw string
	if err /* err 是字符串形式的解析结果。 */ := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*options = nil
		return nil
	}
	if err /* err 是 JSON 字符串内嵌数组的解析结果。 */ := json.Unmarshal([]byte(raw), &direct); err == nil {
		*options = StringList(direct)
		return nil
	}
	// normalized 把常见换行、竖线和中文逗号统一成英文逗号后拆分。
	normalized := strings.NewReplacer("\r\n", ",", "\n", ",", "|", ",", "，", ",").Replace(raw)
	for _, value := range strings.Split(normalized, ",") { // value 是当前远程下拉选项。
		if value = strings.TrimSpace(value); value != "" {
			*options = append(*options, value)
		}
	}
	return nil
}

// PurchaseRequest 是创建一笔幂等远程采购单的请求。
type PurchaseRequest struct {
	InstanceID      int64             `json:"instance_id"`
	ExternalOrderNo string            `json:"external_order_no"`
	XianyuOrderID   string            `json:"xianyu_order_id"`
	RemoteGoodsID   int64             `json:"remote_goods_id"`
	Quantity        int               `json:"quantity"`
	SafePrice       string            `json:"safe_price"`
	CallbackURL     string            `json:"callback_url"`
	Mark            string            `json:"mark"`
	Attach          map[string]string `json:"attach"`
}

// Order 保存本地履约状态和远程订单结果。
type Order struct {
	ID              int64    `json:"id"`
	UserID          int64    `json:"-"`
	InstanceID      int64    `json:"instance_id"`
	ExternalOrderNo string   `json:"external_order_no"`
	RemoteOrderNo   string   `json:"remote_order_no"`
	XianyuOrderID   string   `json:"xianyu_order_id"`
	RemoteGoodsID   int64    `json:"remote_goods_id"`
	Quantity        int      `json:"quantity"`
	Status          int      `json:"status"`
	State           string   `json:"state"`
	TotalPrice      string   `json:"total_price"`
	CardList        []string `json:"card_list,omitempty"`
	RechargeInfo    string   `json:"recharge_info,omitempty"`
	RechargeHints   string   `json:"recharge_hints,omitempty"`
	ErrorMessage    string   `json:"error_message,omitempty"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

// RemoteOrder 是供应商返回的标准化订单结果。
type RemoteOrder struct {
	RemoteOrderNo   string
	ExternalOrderNo string
	Status          int
	TotalPrice      string
	CardList        []string
	RechargeInfo    string
	RechargeHints   string
	// State 是协议适配器给出的统一状态；为空时兼容旧卡速售数字状态映射。
	State string
}

// OrderQuery 提供跨协议查单所需的稳定单号和本地建单时间。
type OrderQuery struct {
	// ExternalOrderNo 是系统生成且重试不变的外部单号。
	ExternalOrderNo string
	// RemoteOrderNo 是供应站已返回的订单号。
	RemoteOrderNo string
	// CreatedAt 是本地幂等订单创建时间，供有最小查单延时的协议使用。
	CreatedAt string
}

// Repository 是履约应用服务消费的持久化窄端口。
type Repository interface {
	ListInstances(context.Context, int64) ([]Instance, error)
	GetInstance(context.Context, int64, int64, bool) (Instance, error)
	GetInstanceByPublicID(context.Context, string, bool) (Instance, error)
	CreateInstance(context.Context, int64, InstanceInput) (Instance, error)
	UpdateInstance(context.Context, int64, int64, InstanceInput) (Instance, error)
	DeleteInstance(context.Context, int64, int64) error
	ListMappings(context.Context, int64) ([]Mapping, error)
	CreateMapping(context.Context, int64, MappingInput) (Mapping, error)
	DeleteMapping(context.Context, int64, int64) error
	CreateOrder(context.Context, int64, PurchaseRequest) (Order, bool, error)
	ApplyRemoteOrder(context.Context, int64, int64, string, RemoteOrder) (Order, error)
	RecordOrderError(context.Context, int64, string, string) error
	GetOrder(context.Context, int64, string) (Order, error)
	ListOrders(context.Context, int64, int) ([]Order, error)
}

// Gateway 定义应用层需要的通用外部货源协议能力。
type Gateway interface {
	ListProducts(context.Context, Instance) ([]Product, error)
	GetProduct(context.Context, Instance, int64) (Product, error)
	Buy(context.Context, Instance, PurchaseRequest) (RemoteOrder, error)
	QueryOrder(context.Context, Instance, OrderQuery) (RemoteOrder, error)
	VerifyOrderCallback(Instance, []byte) (RemoteOrder, error)
}

// NormalizeInstanceInput 对实例输入做不含网络访问的稳定归一化。
func NormalizeInstanceInput(input InstanceInput) InstanceInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Provider = strings.TrimSpace(input.Provider)
	if input.Provider == "" {
		input.Provider = ProviderKasushouV2
	}
	input.BaseURL = strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	input.MerchantUserID = strings.TrimSpace(input.MerchantUserID)
	input.APIKey = strings.TrimSpace(input.APIKey)
	if input.Capabilities.CancelRequestMode == "" {
		input.Capabilities.CancelRequestMode = "callback_url"
	}
	return input
}
