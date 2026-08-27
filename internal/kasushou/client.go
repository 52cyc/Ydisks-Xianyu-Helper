// Package kasushou 实现卡速售 v2 兼容协议，不包含特定站点品牌分支。
package kasushou

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
)

const (
	// defaultTimeout 限制一次货源站 HTTP 请求的总时间。
	defaultTimeout = 15 * time.Second
	// maxResponseBytes 限制远程响应体，避免异常站点消耗过多内存。
	maxResponseBytes = 2 << 20
)

// Clock 提供可测试的毫秒时间戳。
type Clock func() time.Time

// Client 使用官方 v2 签名规则请求任意兼容站。
type Client struct {
	// HTTPClient 是由组合根注入的受限出站客户端。
	HTTPClient *http.Client
	// Now 生成签名所需的 13 位毫秒时间戳。
	Now Clock
}

// NewClient 创建卡速售 v2 协议客户端。
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{HTTPClient: httpClient, Now: time.Now}
}

// ListProducts 请求商品列表。
func (client *Client) ListProducts(ctx context.Context, instance fulfillmentapp.Instance) ([]fulfillmentapp.Product, error) {
	// payloads 是远程 data 字段中的商品列表。
	var payloads productListPayload
	if // callErr 是远程商品列表请求错误。
	callErr := client.call(ctx, instance, "/api/v1/goods/list", map[string]any{}, &payloads); callErr != nil {
		return nil, callErr
	}
	// products 是转换后不含站点特定字段的商品列表。
	products := make([]fulfillmentapp.Product, 0, len(payloads.Items))
	for _,// payload 是当前转换的远程商品。
	payload := range payloads.Items {
		products = append(products, payload.product())
	}
	return products, nil
}

// GetProduct 请求商品详情及动态附加字段。
func (client *Client) GetProduct(ctx context.Context, instance fulfillmentapp.Instance, goodsID int64) (fulfillmentapp.Product, error) {
	// payload 是远程商品详情容器。
	var payload productPayload
	if // callErr 是远程商品详情请求错误。
	callErr := client.call(ctx, instance, "/api/v1/goods/info", map[string]any{"id": goodsID}, &payload); callErr != nil {
		return fulfillmentapp.Product{}, callErr
	}
	// attach 是独立附加字段接口返回的字段列表。
	var attach []fulfillmentapp.AttachField
	if // attachErr 是可选附加字段请求错误，失败时保留详情结果。
	attachErr := client.call(ctx, instance, "/api/v1/goods/attach", map[string]any{"goods_id": strconv.FormatInt(goodsID, 10)}, &attach); attachErr == nil && len(attach) > 0 {
		payload.Attach = attach
	}
	return payload.product(), nil
}

// Buy 使用稳定外部订单号创建远程采购单。
func (client *Client) Buy(ctx context.Context, instance fulfillmentapp.Instance, request fulfillmentapp.PurchaseRequest) (fulfillmentapp.RemoteOrder, error) {
	// body 同时用于签名和发送，防止两份 JSON 发生偏差。
	body := map[string]any{"id": request.RemoteGoodsID, "external_orderno": request.ExternalOrderNo, "quantity": request.Quantity}
	if len(request.Attach) > 0 {
		body["attach"] = request.Attach
	}
	if request.SafePrice != "" {
		body["safe_price"] = request.SafePrice
	}
	if request.CallbackURL != "" {
		body["url"] = request.CallbackURL
	}
	if request.Mark != "" {
		body["mark"] = request.Mark
	}
	// payload 是远程下单响应。
	var payload orderPayload
	if // callErr 是远程下单请求错误。
	callErr := client.call(ctx, instance, "/api/v1/order/buy", body, &payload); callErr != nil {
		return fulfillmentapp.RemoteOrder{}, callErr
	}
	return payload.remoteOrder(), nil
}

// QueryOrder 优先使用外部订单号查询，与幂等下单键保持一致。
func (client *Client) QueryOrder(ctx context.Context, instance fulfillmentapp.Instance, externalOrderNo, remoteOrderNo string) (fulfillmentapp.RemoteOrder, error) {
	// body 是官方二选一订单查询参数。
	body := map[string]any{}
	if strings.TrimSpace(externalOrderNo) != "" {
		body["external_orderno"] = strings.TrimSpace(externalOrderNo)
	} else if strings.TrimSpace(remoteOrderNo) != "" {
		body["ordersn"] = strings.TrimSpace(remoteOrderNo)
	} else {
		return fulfillmentapp.RemoteOrder{}, errors.New("查询订单缺少外部订单号或远程订单号")
	}
	// payloads 是订单查询接口返回的数组。
	var payloads []orderPayload
	if // callErr 是远程订单查询错误。
	callErr := client.call(ctx, instance, "/api/v1/order/info", body, &payloads); callErr != nil {
		return fulfillmentapp.RemoteOrder{}, callErr
	}
	if len(payloads) == 0 {
		return fulfillmentapp.RemoteOrder{}, fulfillmentapp.ErrNotFound
	}
	return payloads[0].remoteOrder(), nil
}

// VerifyOrderCallback 验证 sha1(time+JSON+key)，签名 JSON 排除 sign、card_list 和 express_list。
func (client *Client) VerifyOrderCallback(instance fulfillmentapp.Instance, body []byte) (fulfillmentapp.RemoteOrder, error) {
	// raw 保留回调字段原始 JSON 类型，用于重建签名文本。
	var raw map[string]any
	if // decodeErr 是回调 JSON 解码错误。
	decodeErr := json.Unmarshal(body, &raw); decodeErr != nil {
		return fulfillmentapp.RemoteOrder{}, errors.New("卡速售回调 JSON 无效")
	}
	// receivedSign 是远程传入的签名。
	receivedSign := strings.ToLower(strings.TrimSpace(stringValue(raw["sign"])))
	// callbackTime 是回调签名首部的时间字段。
	callbackTime := stringValue(raw["time"])
	delete(raw, "sign")
	delete(raw, "card_list")
	delete(raw, "express_list")
	// canonical 由 encoding/json 按键的 UTF-8 字节升序输出稳定对象。
	canonical, err := json.Marshal(raw)
	if err != nil {
		return fulfillmentapp.RemoteOrder{}, err
	}
	if receivedSign == "" || receivedSign != sha1Hex(callbackTime+string(canonical)+instance.APIKey) {
		return fulfillmentapp.RemoteOrder{}, errors.New("卡速售回调签名无效")
	}
	// payload 是已通过签名校验的回调订单。
	var payload orderPayload
	if // decodeErr 是验签后订单字段解码错误。
	decodeErr := json.Unmarshal(body, &payload); decodeErr != nil {
		return fulfillmentapp.RemoteOrder{}, errors.New("卡速售回调订单无效")
	}
	return payload.remoteOrder(), nil
}

// call 签名并请求一个卡速售 v2 JSON 接口。
func (client *Client) call(ctx context.Context, instance fulfillmentapp.Instance, path string, body map[string]any, output any) error {
	// canonical 是同时用于签名和 HTTP 请求的稳定 JSON。
	canonical, err := json.Marshal(body)
	if err != nil {
		return err
	}
	// timestamp 是协议要求的 13 位毫秒时间戳。
	timestamp := strconv.FormatInt(client.Now().UnixMilli(), 10)
	// request 是带有动态签名请求头的 JSON POST 请求。
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(instance.BaseURL, "/")+path, bytes.NewReader(canonical))
	if err != nil {
		return fmt.Errorf("创建卡速售请求: %w", err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Timestamp", timestamp)
	request.Header.Set("UserId", instance.MerchantUserID)
	request.Header.Set("Sign", sha1Hex(timestamp+string(canonical)+instance.APIKey))
	// response 是货源站 HTTP 响应，不写入日志。
	response, err := client.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("请求卡速售站失败: %w", err)
	}
	defer response.Body.Close()
	// responseBody 是受限长度的响应内容，仅用于解码。
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("读取卡速售响应: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return errors.New("卡速售响应过大")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("卡速售站返回 HTTP %d", response.StatusCode)
	}
	// envelope 是卡速售统一响应包装。
	var envelope responseEnvelope
	if // decodeErr 是远程统一响应 JSON 解码错误。
	decodeErr := json.Unmarshal(responseBody, &envelope); decodeErr != nil {
		return errors.New("卡速售响应 JSON 无效")
	}
	if !successCode(envelope.Code) {
		// message 是供应站返回的业务错误文本。
		message := firstText(envelope.Message, envelope.MessageAlt)
		if strings.Contains(message, "订单不存在") {
			return fmt.Errorf("%w: %s", fulfillmentapp.ErrNotFound, message)
		}
		if fulfillmentapp.IsSafePriceExceededMessage(message) {
			return fmt.Errorf("%w: %s", fulfillmentapp.ErrSafePriceExceeded, message)
		}
		return fmt.Errorf("卡速售业务失败: %s", message)
	}
	if output == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if // decodeErr 是远程 data 字段解码错误。
	decodeErr := json.Unmarshal(envelope.Data, output); decodeErr != nil {
		return errors.New("卡速售响应 data 格式无效")
	}
	return nil
}

// responseEnvelope 是卡速售兼容站的统一响应结构。
type responseEnvelope struct {
	Code       any             `json:"code"`
	Message    string          `json:"msg"`
	MessageAlt string          `json:"message"`
	Data       json.RawMessage `json:"data"`
}

// productListPayload 兼容 data 直接为数组或包含 list/data 的站点差异。
type productListPayload struct {
	// Items 是解析后的统一商品列表。
	Items []productPayload
}

// UnmarshalJSON 将常见卡速售兼容站列表外形归一化。
func (payload *productListPayload) UnmarshalJSON(data []byte) error {
	// direct 尝试解析官方直接数组形式。
	var direct []productPayload
	if // directErr 是直接数组形式的解码结果。
	directErr := json.Unmarshal(data, &direct); directErr == nil {
		payload.Items = direct
		return nil
	}
	// wrapped 兼容部分部署额外的 list 或 data 包装。
	var wrapped struct {
		List []productPayload `json:"list"`
		Data []productPayload `json:"data"`
	}
	if // wrappedErr 是包装对象形式的解码错误。
	wrappedErr := json.Unmarshal(data, &wrapped); wrappedErr != nil {
		return wrappedErr
	}
	payload.Items = wrapped.List
	if len(payload.Items) == 0 {
		payload.Items = wrapped.Data
	}
	return nil
}

// productPayload 是远程商品响应字段。
type productPayload struct {
	ID        int64                        `json:"id"`
	Name      string                       `json:"goods_name"`
	Image     string                       `json:"goods_img"`
	GoodsType int                          `json:"goods_type"`
	FaceValue json.Number                  `json:"face_value"`
	Price     json.Number                  `json:"goods_price"`
	Status    int                          `json:"status"`
	Stock     int                          `json:"stock_num"`
	CanBuy    any                          `json:"can_buy"`
	Attach    []fulfillmentapp.AttachField `json:"attach"`
}

// product 把远程商品转换为应用层模型。
func (payload productPayload) product() fulfillmentapp.Product {
	return fulfillmentapp.Product{ID: payload.ID, Name: payload.Name, Image: payload.Image, GoodsType: payload.GoodsType, FaceValue: payload.FaceValue.String(), Price: payload.Price.String(), Status: payload.Status, Stock: payload.Stock, CanBuy: boolValue(payload.CanBuy), Attach: payload.Attach}
}

// orderPayload 是下单、查询和回调共用的订单字段。
type orderPayload struct {
	RemoteOrderNo   string              `json:"ordersn"`
	ExternalOrderNo string              `json:"external_orderno"`
	Status          int                 `json:"status"`
	TotalPrice      json.Number         `json:"total_price"`
	CardList        cardListPayload     `json:"card_list"`
	RechargeInfo    rechargeInfoPayload `json:"recharge_info"`
	RechargeHints   string              `json:"recharge_hints"`
}

// remoteOrder 把站点订单转换为应用层统一结果。
func (payload orderPayload) remoteOrder() fulfillmentapp.RemoteOrder {
	return fulfillmentapp.RemoteOrder{RemoteOrderNo: payload.RemoteOrderNo, ExternalOrderNo: payload.ExternalOrderNo, Status: payload.Status, TotalPrice: payload.TotalPrice.String(), CardList: []string(payload.CardList), RechargeInfo: string(payload.RechargeInfo), RechargeHints: payload.RechargeHints}
}

// cardListPayload 兼容卡速售官方对象数组和部分兼容站的字符串数组。
type cardListPayload []string

// UnmarshalJSON 将卡号卡密对象转换为可以直接发送给买家的文本。
func (payload *cardListPayload) UnmarshalJSON(data []byte) error {
	// entries 是卡密列表中的原始 JSON 元素。
	var entries []json.RawMessage
	if err := json.Unmarshal(data, &entries); err != nil { // err 是卡密列表外层 JSON 的解析错误。
		return err
	}
	// cards 保存规范化后的每条卡密文本。
	cards := make([]string, 0, len(entries))
	for _, entry := range entries { // entry 是当前待解析的字符串或卡密对象。
		// direct 是兼容站直接返回的卡密字符串。
		var direct string
		if json.Unmarshal(entry, &direct) == nil {
			if strings.TrimSpace(direct) != "" {
				cards = append(cards, direct)
			}
			continue
		}
		// card 是官方 card_no 与 card_password 结构。
		var card struct {
			Number   string `json:"card_no"`
			Password string `json:"card_password"`
		}
		if err := json.Unmarshal(entry, &card); err != nil { // err 是单条卡密对象的解析错误。
			return err
		}
		// parts 保存当前卡密中非空的卡号和密码。
		parts := make([]string, 0, 2)
		if strings.TrimSpace(card.Number) != "" {
			parts = append(parts, "卡号："+card.Number)
		}
		if strings.TrimSpace(card.Password) != "" {
			parts = append(parts, "卡密："+card.Password)
		}
		if len(parts) > 0 {
			cards = append(cards, strings.Join(parts, "\n"))
		}
	}
	*payload = cards
	return nil
}

// rechargeInfoPayload 兼容直充结果的字符串形式和官方字段数组形式。
type rechargeInfoPayload string

// UnmarshalJSON 将直充字段数组整理为可读文本。
func (payload *rechargeInfoPayload) UnmarshalJSON(data []byte) error {
	// direct 是部分兼容站直接返回的直充结果文本。
	var direct string
	if json.Unmarshal(data, &direct) == nil {
		*payload = rechargeInfoPayload(direct)
		return nil
	}
	// fields 是官方返回的直充字段列表。
	var fields []struct {
		Name  string `json:"n"`
		Value string `json:"v"`
		Key   string `json:"k"`
	}
	if err := json.Unmarshal(data, &fields); err != nil { // err 是直充字段列表的解析错误。
		return err
	}
	// lines 保存可直接展示的直充字段文本。
	lines := make([]string, 0, len(fields))
	for _, field := range fields { // field 是当前直充字段。
		// label 优先使用货源站提供的中文名称。
		label := strings.TrimSpace(field.Name)
		if label == "" {
			label = strings.TrimSpace(field.Key)
		}
		if label != "" || strings.TrimSpace(field.Value) != "" {
			lines = append(lines, label+"："+field.Value)
		}
	}
	*payload = rechargeInfoPayload(strings.Join(lines, "\n"))
	return nil
}

// sha1Hex 返回卡速售协议要求的小写 SHA1 十六进制文本。
func sha1Hex(value string) string {
	// digest 是待签名文本的 SHA1 摘要。
	digest := sha1.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}

// successCode 兼容数字或字符串形式的成功码。
func successCode(value any) bool {
	switch // typed 是待归一化的成功码具体值。
	typed := value.(type) {
	case float64:
		return typed == 0 || typed == 1 || typed == 200
	case string:
		return typed == "0" || typed == "1" || typed == "200" || strings.EqualFold(typed, "success")
	case nil:
		return true
	default:
		return false
	}
}

// stringValue 把回调的字符串或数字字段稳定转为文本。
func stringValue(value any) string {
	switch // typed 是待转换的回调字段具体值。
	typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

// boolValue 兼容布尔、0/1 数字和文本形式的可购状态。
func boolValue(value any) bool {
	switch // typed 是待转换的可购状态具体值。
	typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed != 0
	case string:
		return typed == "1" || strings.EqualFold(typed, "true")
	default:
		return false
	}
}

// firstText 返回第一个非空错误文本。
func firstText(values ...string) string {
	for _,// value 是当前候选错误文本。
	value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "未知错误"
}
