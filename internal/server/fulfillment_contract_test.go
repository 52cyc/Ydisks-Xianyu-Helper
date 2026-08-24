package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
)

// fulfillmentContractPort 为 HTTP 契约测试提供不访问外部站点的确定履约结果。
type fulfillmentContractPort struct{}

// ListInstances 返回一个不暴露密钥的测试实例。
func (fulfillmentContractPort) ListInstances(context.Context, int64) ([]fulfillmentapp.Instance, error) {
	return []fulfillmentapp.Instance{fulfillmentContractInstance()}, nil
}

// CreateInstance 返回已归一化的新建实例。
func (fulfillmentContractPort) CreateInstance(context.Context, int64, fulfillmentapp.InstanceInput) (fulfillmentapp.Instance, error) {
	return fulfillmentContractInstance(), nil
}

// UpdateInstance 返回已更新的测试实例。
func (fulfillmentContractPort) UpdateInstance(context.Context, int64, int64, fulfillmentapp.InstanceInput) (fulfillmentapp.Instance, error) {
	return fulfillmentContractInstance(), nil
}

// DeleteInstance 接受归属内的实例删除请求。
func (fulfillmentContractPort) DeleteInstance(context.Context, int64, int64) error { return nil }

// ListProducts 返回远程商品摘要契约数据。
func (fulfillmentContractPort) ListProducts(context.Context, int64, int64) ([]fulfillmentapp.Product, error) {
	return []fulfillmentapp.Product{fulfillmentContractProduct()}, nil
}

// GetProduct 返回带直充附加字段的商品详情。
func (fulfillmentContractPort) GetProduct(context.Context, int64, int64, int64) (fulfillmentapp.Product, error) {
	return fulfillmentContractProduct(), nil
}

// ListMappings 返回闲鱼商品与货源商品的契约映射。
func (fulfillmentContractPort) ListMappings(context.Context, int64) ([]fulfillmentapp.Mapping, error) {
	return []fulfillmentapp.Mapping{fulfillmentContractMapping()}, nil
}

// CreateMapping 返回已创建的商品映射。
func (fulfillmentContractPort) CreateMapping(context.Context, int64, fulfillmentapp.MappingInput) (fulfillmentapp.Mapping, error) {
	return fulfillmentContractMapping(), nil
}

// DeleteMapping 接受归属内的映射删除请求。
func (fulfillmentContractPort) DeleteMapping(context.Context, int64, int64) error { return nil }

// Purchase 返回幂等下单后的履约订单。
func (fulfillmentContractPort) Purchase(context.Context, int64, fulfillmentapp.PurchaseRequest) (fulfillmentapp.Order, error) {
	return fulfillmentContractOrder(), nil
}

// RefreshOrder 返回查询刷新后的履约订单。
func (fulfillmentContractPort) RefreshOrder(context.Context, int64, string) (fulfillmentapp.Order, error) {
	return fulfillmentContractOrder(), nil
}

// ListOrders 返回当前用户的履约订单列表。
func (fulfillmentContractPort) ListOrders(context.Context, int64, int) ([]fulfillmentapp.Order, error) {
	return []fulfillmentapp.Order{fulfillmentContractOrder()}, nil
}

// ApplyOrderCallback 接受已验签并落库的公开回调。
func (fulfillmentContractPort) ApplyOrderCallback(context.Context, string, []byte) error { return nil }

// fulfillmentContractInstance 构造符合 OpenAPI 必填字段的货源实例。
func fulfillmentContractInstance() fulfillmentapp.Instance {
	return fulfillmentapp.Instance{ID: 1, PublicID: "contract-public", Name: "契约货源", Provider: fulfillmentapp.ProviderKasushouV2, BaseURL: "https://supplier.example", MerchantUserID: "merchant", HasAPIKey: true, Capabilities: fulfillmentapp.Capabilities{OrderList: true, OrderCallback: true, CancelRequestMode: "callback_url"}, Enabled: true, CreatedAt: "2026-08-23T00:00:00Z", UpdatedAt: "2026-08-23T00:00:00Z"}
}

// fulfillmentContractProduct 构造同时覆盖卡密和直充字段形状的商品。
func fulfillmentContractProduct() fulfillmentapp.Product {
	return fulfillmentapp.Product{ID: 101, Name: "契约商品", Image: "https://supplier.example/product.png", GoodsType: fulfillmentapp.GoodsTypeRecharge, FaceValue: "10.00", Price: "8.00", Status: 1, Stock: 20, CanBuy: true, Attach: []fulfillmentapp.AttachField{{Type: "input", Name: "手机号", Key: "mobile", Validation: "^1\\d{10}$", Tip: "请输入手机号"}}}
}

// fulfillmentContractMapping 构造一条可用的规格货源映射。
func fulfillmentContractMapping() fulfillmentapp.Mapping {
	return fulfillmentapp.Mapping{ID: 2, AccountID: "acc1", ItemID: "item-contract", SpecName: "面值", SpecValue: "10元", InstanceID: 1, RemoteGoodsID: 101, GoodsType: fulfillmentapp.GoodsTypeRecharge, SafePrice: "8.00", QuantityMode: "order_quantity", FixedQuantity: 1, AttachMapping: map[string]string{"mobile": "buyer_mobile"}, Enabled: true, CreatedAt: "2026-08-23T00:00:00Z", UpdatedAt: "2026-08-23T00:00:00Z"}
}

// fulfillmentContractOrder 构造一笔已完成的履约订单。
func fulfillmentContractOrder() fulfillmentapp.Order {
	return fulfillmentapp.Order{ID: 3, InstanceID: 1, ExternalOrderNo: "contract-order", RemoteOrderNo: "remote-contract", XianyuOrderID: "xianyu-contract", RemoteGoodsID: 101, Quantity: 1, Status: 3, State: "succeeded", TotalPrice: "8.00", CardList: []string{"CARD-001"}, RechargeInfo: "充值成功", CreatedAt: "2026-08-23T00:00:00Z", UpdatedAt: "2026-08-23T00:00:00Z"}
}

// TestOpenAPIFulfillmentSuccessResponses 验证货源管理、商品、映射、订单与公开回调的真实 Router 响应。
func TestOpenAPIFulfillmentSuccessResponses(t *testing.T) {
	// srv、_、cleanup 分别是测试服务、未使用的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.applications.fulfillment = fulfillmentContractPort{}
	// handler 是注入确定履约结果后的真实 HTTP Router。
	handler := srv.Router()
	// sessionCookie 是访问货源管理端点需要的管理员会话。
	sessionCookie := loginHelper(t, handler)
	// requests 覆盖所有需会话的履约成功 operation。
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/fulfillment/instances", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/fulfillment/instances", strings.NewReader(`{"name":"契约货源","base_url":"https://supplier.example","merchant_user_id":"merchant","api_key":"secret","enabled":true}`)),
		httptest.NewRequest(http.MethodPut, "/api/v1/fulfillment/instances/1", strings.NewReader(`{"name":"契约货源","base_url":"https://supplier.example","merchant_user_id":"merchant","enabled":true}`)),
		httptest.NewRequest(http.MethodDelete, "/api/v1/fulfillment/instances/1", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/fulfillment/instances/1/products", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/fulfillment/instances/1/products/101", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/fulfillment/mappings", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/fulfillment/mappings", strings.NewReader(`{"account_id":"acc1","item_id":"item-contract","instance_id":1,"remote_goods_id":101,"goods_type":2,"enabled":true}`)),
		httptest.NewRequest(http.MethodDelete, "/api/v1/fulfillment/mappings/2", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/fulfillment/orders", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/fulfillment/orders", strings.NewReader(`{"instance_id":1,"external_order_no":"contract-order","remote_goods_id":101,"quantity":1}`)),
		httptest.NewRequest(http.MethodPost, "/api/v1/fulfillment/orders/contract-order/refresh", nil),
	}
	for _, request := range requests { // request 是当前待验证的货源管理请求。
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(sessionCookie)
		// recorder 保存真实 Router 的成功响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
	// callbackRequest 是不使用会话、由供应端签名保护的订单回调。
	callbackRequest := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/kasushou-v2/contract-public/order-callback", strings.NewReader(`{"ordersn":"contract-order"}`))
	callbackRequest.Header.Set("Content-Type", "application/json")
	// callbackRecorder 保存公开回调的纯文本成功响应。
	callbackRecorder := httptest.NewRecorder()
	handler.ServeHTTP(callbackRecorder, callbackRequest)
	assertOpenAPIRecordedSuccessResponse(t, callbackRequest, callbackRecorder)
}
