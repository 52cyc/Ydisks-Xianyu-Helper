package adapter

import (
	"context"
	"strings"
	"testing"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
)

// fulfillmentGatewayStub 返回带协议标识的测试商品。
type fulfillmentGatewayStub struct {
	// productName 是当前协议实现返回的商品名称。
	productName string
}

// ListProducts 返回当前协议的单个测试商品。
func (stub fulfillmentGatewayStub) ListProducts(context.Context, fulfillmentapp.Instance) ([]fulfillmentapp.Product, error) {
	return []fulfillmentapp.Product{{Name: stub.productName}}, nil
}

// GetProduct 返回当前协议的测试商品。
func (stub fulfillmentGatewayStub) GetProduct(context.Context, fulfillmentapp.Instance, int64) (fulfillmentapp.Product, error) {
	return fulfillmentapp.Product{Name: stub.productName}, nil
}

// Buy 返回当前协议的测试订单。
func (stub fulfillmentGatewayStub) Buy(context.Context, fulfillmentapp.Instance, fulfillmentapp.PurchaseRequest) (fulfillmentapp.RemoteOrder, error) {
	return fulfillmentapp.RemoteOrder{RemoteOrderNo: stub.productName}, nil
}

// QueryOrder 返回当前协议的测试订单。
func (stub fulfillmentGatewayStub) QueryOrder(context.Context, fulfillmentapp.Instance, fulfillmentapp.OrderQuery) (fulfillmentapp.RemoteOrder, error) {
	return fulfillmentapp.RemoteOrder{RemoteOrderNo: stub.productName}, nil
}

// VerifyOrderCallback 返回当前协议的测试回调订单。
func (stub fulfillmentGatewayStub) VerifyOrderCallback(fulfillmentapp.Instance, []byte) (fulfillmentapp.RemoteOrder, error) {
	return fulfillmentapp.RemoteOrder{RemoteOrderNo: stub.productName}, nil
}

// TestFulfillmentGatewayRoutesByProvider 验证实例协议决定实际调用的客户端。
func TestFulfillmentGatewayRoutesByProvider(t *testing.T) {
	// gateway 登记两个互不混用的测试协议实现。
	gateway := newFulfillmentGateway(map[string]fulfillmentapp.Gateway{
		fulfillmentapp.ProviderKasushouV2: fulfillmentGatewayStub{productName: "kasushou"},
		fulfillmentapp.ProviderKayixinV3:  fulfillmentGatewayStub{productName: "kayixin"},
		fulfillmentapp.ProviderMifengV1:   fulfillmentGatewayStub{productName: "mifeng"},
	})
	// product、productErr 是卡易信实例路由后的商品和错误。
	product, productErr := gateway.GetProduct(context.Background(), fulfillmentapp.Instance{Provider: fulfillmentapp.ProviderKayixinV3}, 1)
	if productErr != nil || product.Name != "kayixin" {
		t.Fatalf("product=%+v err=%v", product, productErr)
	}
	// mifengProduct、mifengErr 验证蜜蜂汇云协议能按标识路由。
	mifengProduct, mifengErr := gateway.GetProduct(context.Background(), fulfillmentapp.Instance{Provider: fulfillmentapp.ProviderMifengV1}, 1)
	if mifengErr != nil || mifengProduct.Name != "mifeng" {
		t.Fatalf("mifeng product=%+v err=%v", mifengProduct, mifengErr)
	}
	if _, unknownErr := gateway.ListProducts(context.Background(), fulfillmentapp.Instance{Provider: "unknown"}); unknownErr == nil || !strings.Contains(unknownErr.Error(), "不支持") { // unknownErr 是未登记协议的预期错误。
		t.Fatalf("unknown err=%v", unknownErr)
	}
}
