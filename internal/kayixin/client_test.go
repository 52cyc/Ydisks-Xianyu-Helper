package kayixin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
)

// TestClientBuyClassifiesSafePriceExceeded 验证卡易信保护价业务拒绝可被自动化稳定识别为涨价分支。
func TestClientBuyClassifiesSafePriceExceeded(t *testing.T) {
	// server 返回卡易信统一响应中的保护价失败文案。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":2400,"msg":"商品价格高于安全价","data":null}`))
	}))
	defer server.Close()
	// buyErr 必须保留保护价哨兵，供上层重新查询最新售价。
	_, buyErr := NewClient(server.Client()).Buy(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app", APIKey: "secret"}, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 8, ExternalOrderNo: "XY-SAFE", Quantity: 1, SafePrice: "2.80"})
	if !errors.Is(buyErr, fulfillmentapp.ErrSafePriceExceeded) {
		t.Fatalf("保护价错误分类丢失: %v", buyErr)
	}
}

// TestClientBuySignsExactBodyAndConvertsAttach 验证卡易信下单正文、请求头签名和直充字段转换。
func TestClientBuySignsExactBodyAndConvertsAttach(t *testing.T) {
	// appID、secret 是不会访问真实站点的测试鉴权值。
	appID, secret := "test-app", "test-secret"
	// server 校验客户端实际发送的原始字节和鉴权头。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { // writer、request 是本地模拟站响应器和收到的请求。
		// body 是签名必须覆盖的原始 JSON 字节。
		var body json.RawMessage
		if decodeErr := json.NewDecoder(request.Body).Decode(&body); decodeErr != nil { // decodeErr 是测试请求正文解析错误。
			t.Fatal(decodeErr)
		}
		// canonical 是移除解码空白后与发送结构一致的 JSON。
		canonical, _ := json.Marshal(body)
		// timestamp 是客户端发送的固定秒级时间戳。
		timestamp := request.Header.Get("X-Timestamp")
		if request.URL.Path != "/api/v3/order/create" || request.Header.Get("X-APP-ID") != appID || request.Header.Get("X-Version") != "3.0" || timestamp != "1700000000" {
			t.Fatalf("path=%s headers=%v", request.URL.Path, request.Header)
		}
		if request.Header.Get("X-Signature") != md5Hex(appID+secret+"3.0"+timestamp+string(canonical)) {
			t.Fatal("卡易信签名未覆盖实际发送正文")
		}
		// decoded 是用于断言保护价和 attach 形状的请求对象。
		var decoded struct {
			// SafePrice 是订单总保护价。
			SafePrice float64 `json:"safePrice"`
			// Attach 是按名称排序后的直充字段。
			Attach []orderAttach `json:"attach"`
		}
		if decodeErr := json.Unmarshal(canonical, &decoded); decodeErr != nil { // decodeErr 是请求结构断言前的解析错误。
			t.Fatal(decodeErr)
		}
		if decoded.SafePrice != 2.8 || len(decoded.Attach) != 2 || decoded.Attach[0].Name != "充值账号" || decoded.Attach[1].Name != "区服" {
			t.Fatalf("body=%s", canonical)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":1000,"msg":"购买成功","data":{"cards":[],"orderNumber":"KYX-1"}}`))
	}))
	defer server.Close()
	// client 使用固定时钟生成可重复签名。
	client := NewClient(server.Client())
	client.Now = func() time.Time { return time.Unix(1700000000, 0) }
	// result、buyErr 是标准化下单结果和执行错误。
	result, buyErr := client.Buy(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: appID, APIKey: secret}, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 6518, ExternalOrderNo: "xy-1", Quantity: 1, SafePrice: "2.8", Attach: map[string]string{"区服": "一区", "充值账号": "13800000000"}})
	if buyErr != nil || result.RemoteOrderNo != "KYX-1" || result.State != "waiting" || result.ExternalOrderNo != "xy-1" {
		t.Fatalf("result=%+v err=%v", result, buyErr)
	}
}

// TestClientGetProductConvertsRechargeTemplate 验证商品详情类型、价格和直充模板归一化。
func TestClientGetProductConvertsRechargeTemplate(t *testing.T) {
	// server 返回包含下拉模板的单规格直充商品。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { // writer、request 是本地商品详情响应器和请求。
		if request.URL.Path != "/api/v3/goods/getDetail" {
			t.Fatalf("path=%s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":1000,"msg":"success","data":{"goodsId":4994,"name":"测试直充","goodsType":3,"faceValue":100,"salesPrice":0.02,"status":1,"stockCount":9,"skuType":0,"rechargeTemplates":[{"type":14,"title":"区服","placeholder":"请选择","required":1,"regex":"","options":[{"name":"一区","value":"1"},{"name":"二区","value":"2"}]}]}}`))
	}))
	defer server.Close()
	// product、productErr 是归一化商品及读取错误。
	product, productErr := NewClient(server.Client()).GetProduct(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app", APIKey: "secret"}, 4994)
	if productErr != nil || product.ID != 4994 || product.GoodsType != fulfillmentapp.GoodsTypeRecharge || product.Price != "0.02" || !product.CanBuy || len(product.Attach) != 1 || product.Attach[0].Key != "区服" || strings.Join(product.Attach[0].Options, ",") != "一区,二区" {
		t.Fatalf("product=%+v err=%v", product, productErr)
	}
}

// TestClientQueryOrderMapsStatusesAndCards 验证卡易信状态和明文卡券转换。
func TestClientQueryOrderMapsStatusesAndCards(t *testing.T) {
	// server 返回已完成卡密订单。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { // writer、request 是本地查单响应器和请求。
		// body 是断言原外部单号优先使用的查询参数。
		var body orderDetailRequest
		if decodeErr := json.NewDecoder(request.Body).Decode(&body); decodeErr != nil { // decodeErr 是查单请求解析错误。
			t.Fatal(decodeErr)
		}
		if body.OuterNumber != "xy-stable" || body.OrderNumber != "" {
			t.Fatalf("query=%+v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":1000,"msg":"success","data":{"orderNumber":"KYX-2","outerNumber":"xy-stable","status":3,"money":8.8,"result":"处理成功","cards":[{"cardNo":"NO-1","cardPwd":"PWD-1"}]}}`))
	}))
	defer server.Close()
	// result、queryErr 是统一订单结果和查询错误。
	result, queryErr := NewClient(server.Client()).QueryOrder(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app", APIKey: "secret"}, "xy-stable", "KYX-2")
	if queryErr != nil || result.State != "succeeded" || result.TotalPrice != "8.8" || len(result.CardList) != 1 || result.CardList[0] != "卡号：NO-1\n卡密：PWD-1" {
		t.Fatalf("result=%+v err=%v", result, queryErr)
	}
}

// TestClientRejectsMultiSKUAndMapsNotFound 验证未支持规格和远程查无订单都明确失败。
func TestClientRejectsMultiSKUAndMapsNotFound(t *testing.T) {
	// calls 区分商品详情和订单查询两次请求。
	calls := 0
	// server 依次返回多规格商品和订单不存在错误。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { // writer 是本地错误场景响应器。
		calls++
		writer.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = writer.Write([]byte(`{"code":1000,"msg":"success","data":{"goodsId":10,"name":"多规格","skuType":1}}`))
			return
		}
		_, _ = writer.Write([]byte(`{"code":2404,"msg":"订单不存在","data":null}`))
	}))
	defer server.Close()
	// client 是两个错误分支共用的本地协议客户端。
	client := NewClient(server.Client())
	if _, productErr := client.GetProduct(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app", APIKey: "secret"}, 10); productErr == nil || !strings.Contains(productErr.Error(), "多规格") { // productErr 是预期的规格能力错误。
		t.Fatalf("product err=%v", productErr)
	}
	if _, queryErr := client.QueryOrder(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "app", APIKey: "secret"}, "xy-missing", ""); queryErr == nil || !strings.Contains(queryErr.Error(), fulfillmentapp.ErrNotFound.Error()) { // queryErr 是预期的稳定查无订单错误。
		t.Fatalf("query err=%v", queryErr)
	}
}
