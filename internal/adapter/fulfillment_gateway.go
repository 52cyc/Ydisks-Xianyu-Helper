package adapter

import (
	"context"
	"fmt"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
)

// fulfillmentGateway 按货源实例声明的协议选择具体客户端。
type fulfillmentGateway struct {
	// providers 保存协议标识到对应窄网关的固定映射。
	providers map[string]fulfillmentapp.Gateway
}

// newFulfillmentGateway 创建不会在运行期改变的多协议网关。
func newFulfillmentGateway(providers map[string]fulfillmentapp.Gateway) fulfillmentapp.Gateway {
	// copiedProviders 隔离组合根传入映射的后续修改。
	copiedProviders := make(map[string]fulfillmentapp.Gateway, len(providers))
	for provider, gateway := range providers { // provider、gateway 是待登记的协议标识和实现。
		if provider != "" && gateway != nil {
			copiedProviders[provider] = gateway
		}
	}
	return &fulfillmentGateway{providers: copiedProviders}
}

// gatewayFor 返回实例协议对应的实现。
func (gateway *fulfillmentGateway) gatewayFor(instance fulfillmentapp.Instance) (fulfillmentapp.Gateway, error) {
	// selected 是当前实例声明协议对应的实现。
	selected := gateway.providers[instance.Provider]
	if selected == nil {
		return nil, fmt.Errorf("不支持的货源协议: %s", instance.Provider)
	}
	return selected, nil
}

// ListProducts 使用实例对应协议读取商品列表。
func (gateway *fulfillmentGateway) ListProducts(ctx context.Context, instance fulfillmentapp.Instance) ([]fulfillmentapp.Product, error) {
	// selected、selectErr 分别是协议实现和选择错误。
	selected, selectErr := gateway.gatewayFor(instance)
	if selectErr != nil {
		return nil, selectErr
	}
	return selected.ListProducts(ctx, instance)
}

// ListCategories 使用实例对应协议读取商品目录树。
func (gateway *fulfillmentGateway) ListCategories(ctx context.Context, instance fulfillmentapp.Instance) ([]fulfillmentapp.Category, error) {
	// selected、selectErr 分别是协议实现和选择错误。
	selected, selectErr := gateway.gatewayFor(instance)
	if selectErr != nil {
		return nil, selectErr
	}
	// catalog、supported 表示当前协议实现是否支持目录选品扩展。
	catalog, supported := selected.(fulfillmentapp.CatalogGateway)
	if !supported {
		return nil, fmt.Errorf("货源协议 %s 暂不支持目录选品", instance.Provider)
	}
	return catalog.ListCategories(ctx, instance)
}

// ListProductPage 使用实例对应协议按目录和关键词分页读取商品。
func (gateway *fulfillmentGateway) ListProductPage(ctx context.Context, instance fulfillmentapp.Instance, query fulfillmentapp.ProductListQuery) (fulfillmentapp.ProductPage, error) {
	// selected、selectErr 分别是协议实现和选择错误。
	selected, selectErr := gateway.gatewayFor(instance)
	if selectErr != nil {
		return fulfillmentapp.ProductPage{}, selectErr
	}
	// catalog、supported 表示当前协议实现是否支持分页选品扩展。
	catalog, supported := selected.(fulfillmentapp.CatalogGateway)
	if !supported {
		return fulfillmentapp.ProductPage{}, fmt.Errorf("货源协议 %s 暂不支持目录选品", instance.Provider)
	}
	return catalog.ListProductPage(ctx, instance, query)
}

// GetProduct 使用实例对应协议读取商品详情。
func (gateway *fulfillmentGateway) GetProduct(ctx context.Context, instance fulfillmentapp.Instance, goodsID int64) (fulfillmentapp.Product, error) {
	// selected、selectErr 分别是协议实现和选择错误。
	selected, selectErr := gateway.gatewayFor(instance)
	if selectErr != nil {
		return fulfillmentapp.Product{}, selectErr
	}
	return selected.GetProduct(ctx, instance, goodsID)
}

// Buy 使用实例对应协议创建幂等采购单。
func (gateway *fulfillmentGateway) Buy(ctx context.Context, instance fulfillmentapp.Instance, request fulfillmentapp.PurchaseRequest) (fulfillmentapp.RemoteOrder, error) {
	// selected、selectErr 分别是协议实现和选择错误。
	selected, selectErr := gateway.gatewayFor(instance)
	if selectErr != nil {
		return fulfillmentapp.RemoteOrder{}, selectErr
	}
	return selected.Buy(ctx, instance, request)
}

// QueryOrder 使用实例对应协议查询原采购单。
func (gateway *fulfillmentGateway) QueryOrder(ctx context.Context, instance fulfillmentapp.Instance, query fulfillmentapp.OrderQuery) (fulfillmentapp.RemoteOrder, error) {
	// selected、selectErr 分别是协议实现和选择错误。
	selected, selectErr := gateway.gatewayFor(instance)
	if selectErr != nil {
		return fulfillmentapp.RemoteOrder{}, selectErr
	}
	return selected.QueryOrder(ctx, instance, query)
}

// VerifyOrderCallback 使用实例对应协议验证订单回调。
func (gateway *fulfillmentGateway) VerifyOrderCallback(instance fulfillmentapp.Instance, body []byte) (fulfillmentapp.RemoteOrder, error) {
	// selected、selectErr 分别是协议实现和选择错误。
	selected, selectErr := gateway.gatewayFor(instance)
	if selectErr != nil {
		return fulfillmentapp.RemoteOrder{}, selectErr
	}
	return selected.VerifyOrderCallback(instance, body)
}

// 确保多协议路由同时实现基础履约与可选目录选品能力。
var _ fulfillmentapp.Gateway = (*fulfillmentGateway)(nil)
var _ fulfillmentapp.CatalogGateway = (*fulfillmentGateway)(nil)
