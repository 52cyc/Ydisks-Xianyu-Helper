package adapter

import (
	"context"
	"errors"
	"testing"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
	"xianyu-go/internal/automation"
)

// automationFulfillmentGatewayStub 记录自动化首次采购和后续原单查询次数，并模拟供应站短暂查无订单。
type automationFulfillmentGatewayStub struct {
	// buyCalls 是真实远程采购请求次数，恢复流程不得把它增加到二次。
	buyCalls int
	// queryCalls 是使用原外部订单号查询状态的次数。
	queryCalls int
}

// ListProducts 返回空商品列表；当前测试不经过目录接口。
func (stub *automationFulfillmentGatewayStub) ListProducts(context.Context, fulfillmentapp.Instance) ([]fulfillmentapp.Product, error) {
	return nil, nil
}

// GetProduct 返回空商品；当前测试不经过商品详情接口。
func (stub *automationFulfillmentGatewayStub) GetProduct(context.Context, fulfillmentapp.Instance, int64) (fulfillmentapp.Product, error) {
	return fulfillmentapp.Product{}, nil
}

// Buy 模拟供应站已受理采购但仍处于等待状态。
func (stub *automationFulfillmentGatewayStub) Buy(_ context.Context, _ fulfillmentapp.Instance, request fulfillmentapp.PurchaseRequest) (fulfillmentapp.RemoteOrder, error) {
	stub.buyCalls++
	return fulfillmentapp.RemoteOrder{ExternalOrderNo: request.ExternalOrderNo, RemoteOrderNo: "remote-waiting", Status: 1}, nil
}

// QueryOrder 模拟供应站最终一致性窗口内暂时找不到原订单。
func (stub *automationFulfillmentGatewayStub) QueryOrder(context.Context, fulfillmentapp.Instance, fulfillmentapp.OrderQuery) (fulfillmentapp.RemoteOrder, error) {
	stub.queryCalls++
	return fulfillmentapp.RemoteOrder{}, fulfillmentapp.ErrNotFound
}

// VerifyOrderCallback 返回空回调结果；当前测试不经过回调入口。
func (stub *automationFulfillmentGatewayStub) VerifyOrderCallback(fulfillmentapp.Instance, []byte) (fulfillmentapp.RemoteOrder, error) {
	return fulfillmentapp.RemoteOrder{}, nil
}

// TestExternalProductQuoteErrorDistinguishesDeletedProduct 验证只有远程商品删除会转换为自动化保护价哨兵。
func TestExternalProductQuoteErrorDistinguishesDeletedProduct(t *testing.T) {
	// productMissing 是货源应用层已经确认的商品删除错误。
	productMissing := errors.Join(fulfillmentapp.ErrProductNotFound, errors.New("商品不存在"))
	if !errors.Is(externalProductQuoteError(productMissing), automation.ErrExternalProductNotFound) {
		t.Fatal("商品删除错误没有转换为自动化保护价哨兵")
	}
	// instanceMissing 是实例或归属不存在错误，不得触发商品保护价。
	instanceMissing := fulfillmentapp.ErrNotFound
	if errors.Is(externalProductQuoteError(instanceMissing), automation.ErrExternalProductNotFound) {
		t.Fatal("实例不存在被错误转换为商品删除")
	}
}

// TestExternalFulfillmentDelaysFirstQueryAndNeverRepurchases 验证首次等待结果留给恢复扫描，查无原单时也只继续查询而不二次采购。
func TestExternalFulfillmentDelaysFirstQueryAndNeverRepurchases(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "automation-fulfillment-test-key")
	// store、cleanup 分别是隔离数据库及其释放函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是实例、订单和自动化适配器调用共用的上下文。
	ctx := context.Background()
	// owner 是测试货源实例所属管理员。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 保存测试货源实例和稳定外部订单。
	repository := NewFulfillmentRepository(store)
	// instance 是等待态采购使用的启用货源实例。
	instance, instanceErr := repository.CreateInstance(ctx, owner.ID, fulfillmentapp.InstanceInput{Name: "等待站", Provider: fulfillmentapp.ProviderKasushouV2, BaseURL: "https://supplier.example", MerchantUserID: "merchant", APIKey: "secret", Enabled: true})
	if instanceErr != nil {
		t.Fatal(instanceErr)
	}
	// gateway 记录采购和查单次数，查单固定返回暂时不存在。
	gateway := &automationFulfillmentGatewayStub{}
	// adapter 是生产自动化链路使用的履约适配器。
	adapter := newAutomationExternalFulfillmentAdapter(fulfillmentapp.NewService(repository, gateway))
	// request 在两次调用中保持同一个外部订单号和采购参数。
	request := automation.ExternalFulfillmentRequest{UserID: owner.ID, InstanceID: instance.ID, ExternalOrderNo: "xy-order-a1", XianyuOrderID: "order", GoodsID: 46, Quantity: 1}
	// first、firstErr 是首次采购后的等待结果；此时不应立即查单。
	first, firstErr := adapter.Fulfill(ctx, request)
	if firstErr != nil || first.State != "waiting" || gateway.buyCalls != 1 || gateway.queryCalls != 0 {
		t.Fatalf("first=%+v buy=%d query=%d err=%v", first, gateway.buyCalls, gateway.queryCalls, firstErr)
	}
	// second、secondErr 是恢复调用查询原单但供应站暂未查到时的等待结果。
	second, secondErr := adapter.Fulfill(ctx, request)
	if secondErr != nil || second.State != "waiting" || gateway.buyCalls != 1 || gateway.queryCalls != 1 {
		t.Fatalf("second=%+v buy=%d query=%d err=%v", second, gateway.buyCalls, gateway.queryCalls, secondErr)
	}
	// updateErr 模拟后续原单查询已经确认订单取消；自动恢复仍不得创建替代采购单。
	_, updateErr := store.DB.ExecContext(ctx, `UPDATE fulfillment_orders SET state='cancelled' WHERE user_id=? AND external_order_no=?`, owner.ID, request.ExternalOrderNo)
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	// terminalFirst、terminalFirstErr 是首次观察到取消终态后的自动化结果。
	terminalFirst, terminalFirstErr := adapter.Fulfill(ctx, request)
	// terminalSecond、terminalSecondErr 验证终态再次进入自动恢复也不会触发替代采购。
	terminalSecond, terminalSecondErr := adapter.Fulfill(ctx, request)
	if terminalFirstErr != nil || terminalSecondErr != nil || terminalFirst.State != "cancelled" || terminalSecond.State != "cancelled" || gateway.buyCalls != 1 {
		t.Fatalf("terminal first=%+v second=%+v buy=%d first_err=%v second_err=%v", terminalFirst, terminalSecond, gateway.buyCalls, terminalFirstErr, terminalSecondErr)
	}
}
