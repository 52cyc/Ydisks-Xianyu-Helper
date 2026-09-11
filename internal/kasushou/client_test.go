package kasushou

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
)

// TestClientBuyClassifiesSafePriceExceeded 验证卡速售保护价业务拒绝保留稳定错误分类供自动化选择重新报价文案。
func TestClientBuyClassifiesSafePriceExceeded(t *testing.T) {
	// server 返回兼容站常见的保护价拦截业务文案。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":500,"msg":"当前价格超过保护价","data":null}`))
	}))
	defer server.Close()
	// buyErr 应保留应用层保护价哨兵，不能退化成普通供应站异常。
	_, buyErr := NewClient(server.Client()).Buy(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "user", APIKey: "key"}, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 8, ExternalOrderNo: "XY-SAFE", Quantity: 1, SafePrice: "2.80"})
	if !errors.Is(buyErr, fulfillmentapp.ErrSafePriceExceeded) {
		t.Fatalf("保护价错误分类丢失: %v", buyErr)
	}
}

// TestClientBuySignsCanonicalBody 验证下单请求使用 13 位时间戳和稳定 JSON 生成签名。
func TestClientBuySignsCanonicalBody(t *testing.T) {
	// key 是测试货源实例的 API 密钥。
	key := "secret-key"
	// server 检查请求路径、头和正文之间的签名一致性。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/order/buy" {
			t.Fatalf("path=%s", request.URL.Path)
		}
		// body 是服务端收到的原始 JSON，必须与签名文本完全一致。
		var body map[string]any
		if // decodeErr 是测试服务端请求体解码错误。
		decodeErr := json.NewDecoder(request.Body).Decode(&body); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		// canonical 是服务端使用同一 JSON 规则重建的签名正文。
		canonical, _ := json.Marshal(body)
		// digest 是应当出现在 Sign 请求头的摘要。
		digest := sha1.Sum([]byte(request.Header.Get("Timestamp") + string(canonical) + key))
		if request.Header.Get("Sign") != hex.EncodeToString(digest[:]) {
			t.Fatalf("invalid sign")
		}
		if request.Header.Get("Timestamp") != "1700000000123" || request.Header.Get("UserId") != "user-1" {
			t.Fatalf("headers timestamp=%s user=%s", request.Header.Get("Timestamp"), request.Header.Get("UserId"))
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":1,"data":{"ordersn":"KSS1","external_orderno":"XY1","status":1,"total_price":"9.9"}}`))
	}))
	defer server.Close()
	// client 使用固定时间完成可重复签名。
	client := NewClient(server.Client())
	client.Now = func() time.Time { return time.UnixMilli(1700000000123) }
	// result 是转换后的远程订单。
	// result 和 buyErr 是标准化远程订单及下单错误。
	result, buyErr := client.Buy(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "user-1", APIKey: key}, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 8, ExternalOrderNo: "XY1", Quantity: 1, Attach: map[string]string{"account": "13800000000"}})
	if buyErr != nil || result.RemoteOrderNo != "KSS1" || result.ExternalOrderNo != "XY1" {
		t.Fatalf("result=%+v err=%v", result, buyErr)
	}
}

// TestClientBuyOmitsEmptyAttach 验证卡密商品没有直充参数时不发送 attach 空对象。
func TestClientBuyOmitsEmptyAttach(t *testing.T) {
	// server 模拟会拒绝空 attach 的卡速售兼容站。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// body 是服务端收到的下单参数。
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil { // err 是测试请求体解析错误。
			t.Fatal(err)
		}
		if _, exists := body["attach"]; exists { // exists 表示请求错误携带了空 attach 字段。
			t.Fatal("卡密商品不应发送空 attach")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":200,"data":{"ordersn":"KSS2","external_orderno":"XY2","status":1}}`))
	}))
	defer server.Close()
	// client 是本次空 attach 请求使用的协议客户端。
	client := NewClient(server.Client())
	if _, err := client.Buy(context.Background(), fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "user", APIKey: "key"}, fulfillmentapp.PurchaseRequest{RemoteGoodsID: 40863, ExternalOrderNo: "XY2", Quantity: 1}); err != nil { // err 是卡密商品下单错误。
		t.Fatal(err)
	}
}

// TestOrderPayloadAcceptsOfficialCardAndRechargeShapes 验证官方卡密对象和直充数组都能转换成买家可读文本。
func TestOrderPayloadAcceptsOfficialCardAndRechargeShapes(t *testing.T) {
	// payload 是按照官方订单详情示例解析的响应载荷。
	var payload orderPayload
	if err := json.Unmarshal([]byte(`{"ordersn":"KSS3","status":3,"card_list":[{"card_no":"NO-1","card_password":"PWD-1"}],"recharge_info":[{"n":"充值账号","v":"13800000000","k":"recharge_account"}]}`), &payload); err != nil { // err 是官方订单结果解析错误。
		t.Fatal(err)
	}
	// remote 是应用层接收的统一订单结果。
	remote := payload.remoteOrder()
	if len(remote.CardList) != 1 || remote.CardList[0] != "卡号：NO-1\n卡密：PWD-1" || remote.RechargeInfo != "充值账号：13800000000" {
		t.Fatalf("官方订单结果转换错误: %+v", remote)
	}
}

// TestProductPayloadAcceptsStringOptions 验证智客直充商品把 options 返回为空字符串时仍可正常校验。
func TestProductPayloadAcceptsStringOptions(t *testing.T) {
	// payload 是智客商品 4994 的关键响应形状。
	var payload productPayload
	if err := json.Unmarshal([]byte(`{"id":4994,"goods_name":"直充测试","goods_type":2,"face_value":"100.00","goods_price":"0.02","status":1,"stock_num":958,"attach":[{"key":"1","name":"充值账号","type":"text","tip":"请输入充值账号","vali":"all","options":""}]}`), &payload); err != nil { // err 是兼容响应解析错误。
		t.Fatal(err)
	}
	if payload.ID != 4994 || len(payload.Attach) != 1 || payload.Attach[0].Key != "1" || len(payload.Attach[0].Options) != 0 {
		t.Fatalf("智客直充字段转换错误: %+v", payload)
	}
}

// TestClientCatalogUsesCategoryPaginationAndChannelFields 验证目录树、分页参数和官方渠道文本能够稳定归一化。
func TestClientCatalogUsesCategoryPaginationAndChannelFields(t *testing.T) {
	// server 按请求路径返回官方目录或商品列表外形。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/v1/goods/cate" {
			_, _ = writer.Write([]byte(`{"code":1,"data":[{"id":10,"cate_name":"会员","children":[{"id":11,"cate_name":"视频"}]}]}`))
			return
		}
		// body 是商品列表接口收到的目录和分页条件。
		var body map[string]any
		if decodeErr := json.NewDecoder(request.Body).Decode(&body); decodeErr != nil { // decodeErr 是测试请求体解析错误。
			t.Fatal(decodeErr)
		}
		if body["cate_id"] != float64(11) || body["page"] != float64(2) || body["limit"] != float64(20) || body["keyword"] != "月卡" {
			t.Fatalf("商品分页参数错误: %#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":1,"data":{"list":[{"id":9,"goods_name":"视频月卡","goods_img":"https://img.example/a.jpg","goods_type":1,"goods_price":2.8,"status":1,"stock_num":5,"can_buy":"拼多多,京东","can_no_buy":"淘宝","can_price":1,"need_balance":1}],"total":31}}`))
	}))
	defer server.Close()
	// client 是使用本地确定响应的卡速售协议客户端。
	client := NewClient(server.Client())
	// instance 只包含本地测试服务请求所需的协议身份。
	instance := fulfillmentapp.Instance{BaseURL: server.URL, MerchantUserID: "user", APIKey: "key"}
	// categories、categoryErr 是目录树及请求错误。
	categories, categoryErr := client.ListCategories(context.Background(), instance)
	if categoryErr != nil || len(categories) != 1 || categories[0].Children[0].ID != 11 {
		t.Fatalf("目录归一化失败: categories=%+v err=%v", categories, categoryErr)
	}
	// page、pageErr 是商品分页结果及请求错误。
	page, pageErr := client.ListProductPage(context.Background(), instance, fulfillmentapp.ProductListQuery{CategoryID: 11, Keyword: "月卡", Page: 2, PageSize: 20})
	if pageErr != nil || page.Total != 31 || len(page.Items) != 1 || !page.Items[0].CanBuy || page.Items[0].BuyChannels != "拼多多,京东" || !page.Items[0].CanSetPrice {
		t.Fatalf("商品分页归一化失败: page=%+v err=%v", page, pageErr)
	}
}

// TestVerifyOrderCallbackExcludesSensitiveLists 验证回调签名排除卡密和物流列表。
func TestVerifyOrderCallbackExcludesSensitiveLists(t *testing.T) {
	// key 是回调验签使用的实例密钥。
	key := "callback-key"
	// signed 是排除三个特殊字段后的签名对象。
	signed := map[string]any{"external_orderno": "XY2", "ordersn": "KSS2", "status": float64(3), "time": "1700000000"}
	// canonical 是卡速售回调签名使用的排序 JSON。
	canonical, _ := json.Marshal(signed)
	// digest 是有效回调签名。
	digest := sha1.Sum([]byte("1700000000" + string(canonical) + key))
	// callback 是完整回调，卡密在签名外仍应被正常解析。
	callback := map[string]any{"external_orderno": "XY2", "ordersn": "KSS2", "status": 3, "time": "1700000000", "card_list": []string{"CARD-A"}, "express_list": []any{}, "sign": hex.EncodeToString(digest[:])}
	// body 是模拟 HTTP 回调的 JSON 正文。
	body, _ := json.Marshal(callback)
	// result 是验签通过后的标准订单。
	// result 和 verifyErr 是回调验签后订单及验证错误。
	result, verifyErr := NewClient(nil).VerifyOrderCallback(fulfillmentapp.Instance{APIKey: key}, body)
	if verifyErr != nil || result.Status != 3 || len(result.CardList) != 1 {
		t.Fatalf("result=%+v err=%v", result, verifyErr)
	}
	callback["sign"] = "bad"
	body, _ = json.Marshal(callback)
	if // invalidErr 是错误签名应当触发的验证错误。
	_, invalidErr := NewClient(nil).VerifyOrderCallback(fulfillmentapp.Instance{APIKey: key}, body); invalidErr == nil {
		t.Fatal("invalid signature should fail")
	}
}
