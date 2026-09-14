package mifeng

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
)

const testSecret = "test-secret" // testSecret 是测试请求使用的签名密钥。

// TestClientGetProductAndBuyCard 验证卡密商品校验、签名和下单参数。
func TestClientGetProductAndBuyCard(t *testing.T) {
	// fixedNow 固定时间以稳定签名和查单测试。
	fixedNow := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	// productCalls 统计下单前是否重新校验商品。
	var productCalls atomic.Int32
	// server 模拟蜜蜂商品和放单接口。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// body 是完成公共字段及签名校验后的请求体。
		body := decodeSignedBody(t, request)
		switch request.URL.Path {
		case "/api/merchant/productGoodsListNew":
			productCalls.Add(1)
			if body["miniunit_id"] != "10086" {
				t.Fatalf("miniunit_id=%v", body["miniunit_id"])
			}
			writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"count": 1, "data": []map[string]any{{"b_id": 13, "miniunit_id": 10086, "goods_name": "测试卡密", "spec": "10元", "amount": 10, "status": 1, "goods_price": "8.50"}}}})
		case "/api/merchant/upload_order":
			// datas 是不参与签名的采购业务参数。
			datas := body["datas"].(map[string]any)
			if textValue(datas["num"]) != "2" || textValue(datas["maxAmount"]) != "18.00" {
				t.Fatalf("datas=%v", datas)
			}
			if body["third_id"] != "xy-card" {
				t.Fatalf("third_id=%v", body["third_id"])
			}
			writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"third_id": "xy-card", "order_id": "MF-1", "state": 1}})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	// client 是连接模拟服务的蜜蜂客户端。
	client := NewClient(server.Client())
	client.Now = func() time.Time { return fixedNow }
	// instance 是测试使用的货源实例凭据。
	instance := fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app-key", APIKey: testSecret}
	// product、productErr 是商品校验结果和错误。
	product, productErr := client.GetProduct(context.Background(), instance, 10086)
	if productErr != nil || product.GoodsType != fulfillmentapp.GoodsTypeCard || product.Price != "8.50" || !product.CanBuy {
		t.Fatalf("product=%+v err=%v", product, productErr)
	}
	// order、buyErr 是卡密采购结果和错误。
	order, buyErr := client.Buy(context.Background(), instance, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 10086, ExternalOrderNo: "xy-card", Quantity: 2, SafePrice: "18.00"})
	if buyErr != nil || order.RemoteOrderNo != "MF-1" || order.State != "waiting" {
		t.Fatalf("order=%+v err=%v", order, buyErr)
	}
	if productCalls.Load() != 2 {
		t.Fatalf("product calls=%d want=2", productCalls.Load())
	}
}

// TestClientGetProductClassifiesMissingListItem 验证按商品编号查询空列表时产生专用商品删除错误。
func TestClientGetProductClassifiesMissingListItem(t *testing.T) {
	// server 返回没有任何匹配商品的正常列表响应。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { // writer 是空商品列表响应器。
		writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"count": 0, "data": []any{}}})
	}))
	defer server.Close()
	// productErr 是空列表转换后的商品删除错误。
	_, productErr := NewClient(server.Client()).GetProduct(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app-key", APIKey: testSecret}, 5435)
	if !errors.Is(productErr, fulfillmentapp.ErrProductNotFound) {
		t.Fatalf("空商品列表分类=%v", productErr)
	}
}

// TestClientGetDiningProductAsCard 验证餐饮代下商品无需充值账号并按卡券链接履约。
func TestClientGetDiningProductAsCard(t *testing.T) {
	// server 模拟蜜蜂返回 b_id=18 的餐饮商品及放单结果。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// body 是完成公共字段及签名校验后的请求体。
		body := decodeSignedBody(t, request)
		switch request.URL.Path {
		case "/api/merchant/productGoodsListNew":
			writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"count": 1, "data": []map[string]any{{"b_id": 18, "miniunit_id": 1002821, "goods_name": "麦当劳", "spec": "圆筒冰淇淋", "status": 1, "goods_price": "4.070"}}}})
		case "/api/merchant/upload_order":
			// datas 是餐饮卡券采购参数，不应包含直充账号。
			datas := body["datas"].(map[string]any)
			if _, exists := datas["target"]; exists { // exists 用于阻止餐饮卡券误收买家充值账号。
				t.Fatalf("餐饮卡券不应提交 target: %v", datas)
			}
			if textValue(datas["num"]) != "1" {
				t.Fatalf("datas=%v", datas)
			}
			writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"third_id": "xy-food", "order_id": "MF-food", "state": 1}})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	// client 是连接模拟服务的蜜蜂客户端。
	client := NewClient(server.Client())
	// instance 是测试使用的货源实例凭据。
	instance := fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app-key", APIKey: testSecret}
	// product、productErr 是餐饮商品校验结果和错误。
	product, productErr := client.GetProduct(context.Background(), instance, 1002821)
	if productErr != nil || product.GoodsType != fulfillmentapp.GoodsTypeCard || len(product.Attach) != 0 {
		t.Fatalf("product=%+v err=%v", product, productErr)
	}
	// order、buyErr 是无需充值账号的餐饮卡券采购结果和错误。
	order, buyErr := client.Buy(context.Background(), instance, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 1002821, ExternalOrderNo: "xy-food", Quantity: 1, SafePrice: "4.50"})
	if buyErr != nil || order.RemoteOrderNo != "MF-food" || order.State != "waiting" {
		t.Fatalf("order=%+v err=%v", order, buyErr)
	}
}

// TestClientBuyRechargeRequiresTarget 验证直充商品必须提供充值账号。
func TestClientBuyRechargeRequiresTarget(t *testing.T) {
	// server 模拟返回可采购的直充商品。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = decodeSignedBody(t, request)
		writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"count": 1, "data": []map[string]any{{"b_id": 3, "miniunit_id": 4994, "goods_name": "会员直充", "status": 1, "goods_price": "9.90"}}}})
	}))
	defer server.Close()
	// client 是连接模拟服务的蜜蜂客户端。
	client := NewClient(server.Client())
	// instance 是测试使用的货源实例凭据。
	instance := fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app-key", APIKey: testSecret}
	// buyErr 是缺少充值账号时预期返回的错误。
	_, buyErr := client.Buy(context.Background(), instance, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 4994, ExternalOrderNo: "xy-recharge", Quantity: 1})
	if buyErr == nil || !strings.Contains(buyErr.Error(), "datas.target") {
		t.Fatalf("buy err=%v", buyErr)
	}
}

// TestClientQueryHonorsDelayAndMissingGrace 验证首次查询间隔和订单不存在宽限。
func TestClientQueryHonorsDelayAndMissingGrace(t *testing.T) {
	// fixedNow 固定查询时钟。
	fixedNow := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	// calls 统计实际发出的远程查单次数。
	var calls atomic.Int32
	// server 模拟蜜蜂订单不存在响应。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		_ = decodeSignedBody(t, request)
		writeJSON(t, writer, map[string]any{"code": 10015, "message": "订单不存在", "data": nil})
	}))
	defer server.Close()
	// client 是连接模拟服务的蜜蜂客户端。
	client := NewClient(server.Client())
	client.Now = func() time.Time { return fixedNow }
	// instance 是测试使用的货源实例凭据。
	instance := fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app-key", APIKey: testSecret}
	// order、earlyErr 是未满一分钟时的本地等待结果。
	order, earlyErr := client.QueryOrder(context.Background(), instance, fulfillmentapp.OrderQuery{ExternalOrderNo: "xy-delay", CreatedAt: fixedNow.Add(-30 * time.Second).Format(time.RFC3339)})
	if earlyErr != nil || order.State != "waiting" || calls.Load() != 0 {
		t.Fatalf("early order=%+v err=%v calls=%d", order, earlyErr, calls.Load())
	}
	// order、graceErr 是五分钟宽限内的等待结果。
	order, graceErr := client.QueryOrder(context.Background(), instance, fulfillmentapp.OrderQuery{ExternalOrderNo: "xy-delay", CreatedAt: fixedNow.Add(-2 * time.Minute).Format(time.RFC3339)})
	if graceErr != nil || order.State != "waiting" || calls.Load() != 1 {
		t.Fatalf("grace order=%+v err=%v calls=%d", order, graceErr, calls.Load())
	}
	// missingErr 是超过宽限后确认订单不存在的错误。
	_, missingErr := client.QueryOrder(context.Background(), instance, fulfillmentapp.OrderQuery{ExternalOrderNo: "xy-delay", CreatedAt: fixedNow.Add(-6 * time.Minute).Format(time.RFC3339)})
	if !errors.Is(missingErr, fulfillmentapp.ErrNotFound) || calls.Load() != 2 {
		t.Fatalf("missing err=%v calls=%d", missingErr, calls.Load())
	}
}

// TestClientQueryFormatsCards 验证成功订单的卡号、卡密和链接格式。
func TestClientQueryFormatsCards(t *testing.T) {
	// fixedNow 固定查询时钟。
	fixedNow := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	// server 模拟包含两种卡券格式的成功订单。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// body 是完成签名校验后的查单请求。
		body := decodeSignedBody(t, request)
		if body["order_id"] != "MF-2" {
			t.Fatalf("order_id=%v", body["order_id"])
		}
		writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"state": 3, "order_id": "MF-2", "third_id": "xy-card", "cost": "8.50", "ys_cards": []map[string]any{{"card_type": 1, "card_no": "NO-1", "card_pwd": "PWD-1"}, {"card_type": 3, "card_pwd": "https://card.example/redeem"}}}})
	}))
	defer server.Close()
	// client 是连接模拟服务的蜜蜂客户端。
	client := NewClient(server.Client())
	client.Now = func() time.Time { return fixedNow }
	// order、queryErr 是格式化后的统一订单和错误。
	order, queryErr := client.QueryOrder(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app-key", APIKey: testSecret}, fulfillmentapp.OrderQuery{ExternalOrderNo: "xy-card", RemoteOrderNo: "MF-2", CreatedAt: fixedNow.Add(-2 * time.Minute).Format(time.RFC3339)})
	if queryErr != nil || order.State != "succeeded" || len(order.CardList) != 2 {
		t.Fatalf("order=%+v err=%v", order, queryErr)
	}
	if order.CardList[0] != "卡号：NO-1\n卡密：PWD-1" || order.CardList[1] != "兑换链接：https://card.example/redeem" {
		t.Fatalf("cards=%v", order.CardList)
	}
}

// TestClientDuplicateOrderQueriesOriginal 验证重复 third_id 只查询原单且不换号重购。
func TestClientDuplicateOrderQueriesOriginal(t *testing.T) {
	// uploadCalls 统计放单请求次数。
	var uploadCalls atomic.Int32
	// server 模拟重复单号后可从订单列表找到原单。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// body 是完成签名校验后的当前请求体。
		body := decodeSignedBody(t, request)
		switch request.URL.Path {
		case "/api/merchant/productGoodsListNew":
			writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"count": 1, "data": []map[string]any{{"b_id": 13, "miniunit_id": 10086, "goods_name": "测试卡密", "status": 1, "goods_price": "8.50"}}}})
		case "/api/merchant/upload_order":
			uploadCalls.Add(1)
			writeJSON(t, writer, map[string]any{"code": 10010, "message": "第三方订单号已存在", "data": nil})
		case "/api/merchant/order_list":
			if body["third_id"] != "xy-duplicate" {
				t.Fatalf("third_id=%v", body["third_id"])
			}
			writeJSON(t, writer, map[string]any{"code": 0, "message": "ok", "data": map[string]any{"count": 1, "list": []map[string]any{{"state": 3, "order_id": "MF-old", "third_id": "xy-duplicate", "cost": "8.50", "ys_cards": []map[string]any{{"card_type": 2, "card_pwd": "CODE-1"}}}}}})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	// client 是连接模拟服务的蜜蜂客户端。
	client := NewClient(server.Client())
	// order、buyErr 是重复下单后查询到的原订单和错误。
	order, buyErr := client.Buy(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app-key", APIKey: testSecret}, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 10086, ExternalOrderNo: "xy-duplicate", Quantity: 1})
	if buyErr != nil || order.RemoteOrderNo != "MF-old" || order.State != "succeeded" || uploadCalls.Load() != 1 {
		t.Fatalf("order=%+v err=%v upload_calls=%d", order, buyErr, uploadCalls.Load())
	}
}

// decodeSignedBody 解码测试请求并校验公共参数和签名。
func decodeSignedBody(t *testing.T, request *http.Request) map[string]any {
	t.Helper()
	defer request.Body.Close()
	// decoder 保留 JSON 数字文本，避免测试比较精度变化。
	decoder := json.NewDecoder(request.Body)
	decoder.UseNumber()
	// body 是解码后的蜜蜂请求体。
	var body map[string]any
	if decodeErr := decoder.Decode(&body); decodeErr != nil { // decodeErr 是请求体解码错误。
		t.Fatal(decodeErr)
	}
	if body["app_key"] != "app-key" || strings.TrimSpace(textValue(body["timestamp"])) == "" {
		t.Fatalf("common params=%v", body)
	}
	if body["sign"] != requestSign(body, testSecret) {
		t.Fatalf("invalid sign=%v", body["sign"])
	}
	return body
}

// writeJSON 向模拟服务写入 JSON 响应。
func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if encodeErr := json.NewEncoder(writer).Encode(value); encodeErr != nil { // encodeErr 是测试响应编码错误。
		t.Fatal(encodeErr)
	}
}
