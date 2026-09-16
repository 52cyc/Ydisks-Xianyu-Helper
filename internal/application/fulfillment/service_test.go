package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// serviceRepositoryStub 为应用服务测试保存一笔幂等订单。
type serviceRepositoryStub struct {
	// instance 是测试外部请求使用的货源实例。
	instance Instance
	// order 是当前已持久化的幂等订单。
	order Order
	// createCalls 记录本地幂等创建被请求的次数。
	createCalls int
}

// ListInstances 返回空测试实例列表。
func (stub *serviceRepositoryStub) ListInstances(context.Context, int64) ([]Instance, error) {
	return nil, nil
}

// GetInstance 返回测试货源实例。
func (stub *serviceRepositoryStub) GetInstance(context.Context, int64, int64, bool) (Instance, error) {
	return stub.instance, nil
}

// GetInstanceByPublicID 返回测试回调货源实例。
func (stub *serviceRepositoryStub) GetInstanceByPublicID(context.Context, string, bool) (Instance, error) {
	return stub.instance, nil
}

// CreateInstance 返回空测试实例。
func (stub *serviceRepositoryStub) CreateInstance(context.Context, int64, InstanceInput) (Instance, error) {
	return Instance{}, nil
}

// UpdateInstance 返回空测试实例。
func (stub *serviceRepositoryStub) UpdateInstance(context.Context, int64, int64, InstanceInput) (Instance, error) {
	return Instance{}, nil
}

// DeleteInstance 模拟成功删除实例。
func (stub *serviceRepositoryStub) DeleteInstance(context.Context, int64, int64) error { return nil }

// ListMappings 返回空测试映射列表。
func (stub *serviceRepositoryStub) ListMappings(context.Context, int64) ([]Mapping, error) {
	return nil, nil
}

// CreateMapping 返回空测试映射。
func (stub *serviceRepositoryStub) CreateMapping(context.Context, int64, MappingInput) (Mapping, error) {
	return Mapping{}, nil
}

// DeleteMapping 模拟成功删除映射。
func (stub *serviceRepositoryStub) DeleteMapping(context.Context, int64, int64) error { return nil }

// CreateOrder 首次创建幂等订单，后续请求返回原记录。
func (stub *serviceRepositoryStub) CreateOrder(_ context.Context, userID int64, request PurchaseRequest) (Order, bool, error) {
	stub.createCalls++
	if stub.order.ID != 0 && stub.order.ExternalOrderNo == request.ExternalOrderNo {
		return stub.order, false, nil
	}
	stub.order = Order{ID: 1, UserID: userID, InstanceID: request.InstanceID, ExternalOrderNo: request.ExternalOrderNo, RemoteGoodsID: request.RemoteGoodsID, Quantity: request.Quantity, State: "created"}
	return stub.order, true, nil
}

// ApplyRemoteOrder 将远程测试状态写入内存订单。
func (stub *serviceRepositoryStub) ApplyRemoteOrder(_ context.Context, _ int64, _ int64, _ string, remote RemoteOrder) (Order, error) {
	stub.order.RemoteOrderNo, stub.order.Status, stub.order.State = remote.RemoteOrderNo, remote.Status, "waiting"
	return stub.order, nil
}

// RecordOrderError 保存测试订单最近一次错误。
func (stub *serviceRepositoryStub) RecordOrderError(_ context.Context, _ int64, _ string, message string) error {
	stub.order.ErrorMessage = message
	return nil
}

// GetOrder 返回内存测试订单。
func (stub *serviceRepositoryStub) GetOrder(context.Context, int64, string) (Order, error) {
	return stub.order, nil
}

// ListOrders 返回内存测试订单列表。
func (stub *serviceRepositoryStub) ListOrders(context.Context, int64, int) ([]Order, error) {
	return []Order{stub.order}, nil
}

// serviceGatewayStub 记录远程下单次数并可注入结果未知错误。
type serviceGatewayStub struct {
	// buyCalls 是真正发起远程采购的次数。
	buyCalls int
	// buyError 是远程超时或传输失败。
	buyError error
	// productError 是远程商品详情查询的预置错误。
	productError error
}

// ListProducts 返回空测试商品列表。
func (stub *serviceGatewayStub) ListProducts(context.Context, Instance) ([]Product, error) {
	return nil, nil
}

// GetProduct 返回空测试商品。
func (stub *serviceGatewayStub) GetProduct(context.Context, Instance, int64) (Product, error) {
	return Product{}, stub.productError
}

// TestGetProductPreservesRemoteProductMissingClassification 验证应用服务只透传供应商网关已经确认的商品删除语义。
func TestGetProductDistinguishesRemoteProductMissing(t *testing.T) {
	// service 是实例查询成功但远程商品已删除的应用服务。
	service := NewService(&serviceRepositoryStub{instance: Instance{ID: 8}}, &serviceGatewayStub{productError: fmt.Errorf("%w: 商品不存在", ErrProductNotFound)})
	// productErr 保存商品详情查询返回的专用不存在错误。
	_, productErr := service.GetProduct(context.Background(), 1, 8, 5435)
	if !errors.Is(productErr, ErrProductNotFound) {
		t.Fatalf("远程商品不存在分类=%v", productErr)
	}
	// genericService 模拟网关只报告泛资源不存在，应用服务不得自行升级为商品删除。
	genericService := NewService(&serviceRepositoryStub{instance: Instance{ID: 8}}, &serviceGatewayStub{productError: ErrNotFound})
	// genericErr 保存泛资源不存在的透传结果。
	_, genericErr := genericService.GetProduct(context.Background(), 1, 8, 5435)
	if errors.Is(genericErr, ErrProductNotFound) {
		t.Fatalf("泛资源不存在被误判为商品删除: %v", genericErr)
	}
}

// Buy 记录外部副作用并返回测试配置的错误。
func (stub *serviceGatewayStub) Buy(context.Context, Instance, PurchaseRequest) (RemoteOrder, error) {
	stub.buyCalls++
	if stub.buyError != nil {
		return RemoteOrder{}, stub.buyError
	}
	return RemoteOrder{RemoteOrderNo: "KSS-1", Status: 1}, nil
}

// QueryOrder 返回空测试查询结果。
func (stub *serviceGatewayStub) QueryOrder(context.Context, Instance, OrderQuery) (RemoteOrder, error) {
	return RemoteOrder{}, nil
}

// VerifyOrderCallback 返回空测试回调结果。
func (stub *serviceGatewayStub) VerifyOrderCallback(Instance, []byte) (RemoteOrder, error) {
	return RemoteOrder{}, nil
}

// TestPurchaseDoesNotRepeatRemoteBuyAfterUnknownResult 验证超时重试只返回原本地订单。
func TestPurchaseDoesNotRepeatRemoteBuyAfterUnknownResult(t *testing.T) {
	// repository 是保存采购幂等键的测试仓储。
	repository := &serviceRepositoryStub{instance: Instance{ID: 8, Enabled: true}}
	// gateway 模拟第一次远程下单超时且结果未知。
	gateway := &serviceGatewayStub{buyError: errors.New("timeout")}
	// service 是本测试的幂等采购应用服务。
	service := NewService(repository, gateway)
	// request 是两次调用共用的稳定采购参数。
	request := PurchaseRequest{InstanceID: 8, ExternalOrderNo: "XY-STABLE", RemoteGoodsID: 9, Quantity: 1}
	if // purchaseErr 是第一次远程结果未知错误。
	_, submitted, purchaseErr := service.PurchaseWithSubmission(context.Background(), 1, request); purchaseErr == nil || !submitted {
		t.Fatal("first unknown remote result should be reported")
	}
	// secondOrder 是重试时返回的原本地订单。
	secondOrder, submitted, err := service.PurchaseWithSubmission(context.Background(), 1, request)
	if err != nil || submitted || secondOrder.ExternalOrderNo != request.ExternalOrderNo || gateway.buyCalls != 1 {
		t.Fatalf("second=%+v submitted=%v calls=%d err=%v", secondOrder, submitted, gateway.buyCalls, err)
	}
}

// TestReplaceTerminalOrderCreatesStableRevision 验证人工确认后为已取消订单创建递增替代单号。
func TestReplaceTerminalOrderCreatesStableRevision(t *testing.T) {
	// repository 保存已标记为可替代采购的取消订单。
	repository := &serviceRepositoryStub{instance: Instance{ID: 3}, order: Order{ID: 1, UserID: 7, InstanceID: 3, ExternalOrderNo: "xy-order-a1", RemoteGoodsID: 40863, Quantity: 1, State: "cancelled", ErrorMessage: terminalRetryMarker}}
	// gateway 记录替代采购的真实下单次数。
	gateway := &serviceGatewayStub{}
	// service 是本次替代采购使用的应用服务。
	service := NewService(repository, gateway)
	// replacement、err 分别是替代订单和创建错误。
	replacement, err := service.ReplaceTerminalOrder(context.Background(), 7, PurchaseRequest{InstanceID: 3, ExternalOrderNo: "xy-order-a1", RemoteGoodsID: 40863, Quantity: 1, SafePrice: "2.80"})
	if err != nil || gateway.buyCalls != 1 || replacement.ExternalOrderNo != "xy-order-a1-r2" {
		t.Fatalf("替代采购错误: order=%+v calls=%d err=%v", replacement, gateway.buyCalls, err)
	}
}

// TestValidateInstanceInputAcceptsSupportedProviders 验证卡速售和卡易信均可创建，未知协议仍被拒绝。
func TestValidateInstanceInputAcceptsSupportedProviders(t *testing.T) {
	// baseInput 是两个受支持协议共用的完整实例输入。
	baseInput := InstanceInput{Name: "测试货源", BaseURL: "https://supplier.example", MerchantUserID: "merchant", APIKey: "secret"}
	for _, provider := range []string{ProviderKasushouV2, ProviderKayixinV3, ProviderMifengV1} { // provider 是当前待验证的受支持协议。
		// input 是写入当前协议后的独立测试输入。
		input := baseInput
		input.Provider = provider
		if validateErr := validateInstanceInput(input, false); validateErr != nil { // validateErr 是受支持协议不应产生的校验错误。
			t.Fatalf("provider=%s err=%v", provider, validateErr)
		}
	}
	// unknownInput 是应被拒绝的未知协议输入。
	unknownInput := baseInput
	unknownInput.Provider = "unknown"
	if validateErr := validateInstanceInput(unknownInput, false); validateErr == nil { // validateErr 是未知协议的预期校验错误。
		t.Fatal("未知货源协议不应通过校验")
	}
}
