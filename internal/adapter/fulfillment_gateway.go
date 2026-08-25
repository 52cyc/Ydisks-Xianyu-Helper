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
func (gateway *fulfillmentGateway) QueryOrder(ctx context.Context, instance fulfillmentapp.Instance, externalOrderNo, remoteOrderNo string) (fulfillmentapp.RemoteOrder, error) {
	// selected、selectErr 分别是协议实现和选择错误。
	selected, selectErr := gateway.gatewayFor(instance)
	if selectErr != nil {
		return fulfillmentapp.RemoteOrder{}, selectErr
	}
	return selected.QueryOrder(ctx, instance, externalOrderNo, remoteOrderNo)
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
