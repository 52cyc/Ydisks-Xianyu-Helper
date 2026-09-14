// Package mifeng 实现蜜蜂汇云商户采购 API 协议。
package mifeng

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
)

const (
	defaultTimeout       = 15 * time.Second // defaultTimeout 限制单次请求总时间。
	maxResponseBytes     = 2 << 20          // maxResponseBytes 限制远程响应体大小。
	productPageSize      = 10               // productPageSize 遵循蜜蜂商品列表每页上限。
	maxProductPages      = 100              // maxProductPages 防止异常分页无限循环。
	minimumQueryDelay    = time.Minute      // minimumQueryDelay 是蜜蜂要求的首次查单延时。
	missingOrderGrace    = 5 * time.Minute  // missingOrderGrace 是订单不存在的判定宽限。
	maximumPurchaseCount = 10               // maximumPurchaseCount 是蜜蜂文档的单次数量上限。
)

// Clock 提供可测试的秒级签名时间。
type Clock func() time.Time

// Client 使用 AppKey 和 AppSecret 签名访问蜜蜂汇云。
type Client struct {
	HTTPClient *http.Client
	Now        Clock
}

// NewClient 创建蜜蜂汇云客户端。
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{HTTPClient: httpClient, Now: time.Now}
}

// ListProducts 分页读取当前商户可见的蜜蜂商品。
func (client *Client) ListProducts(ctx context.Context, instance fulfillmentapp.Instance) ([]fulfillmentapp.Product, error) {
	// products 保存逐页转换的统一商品。
	products := make([]fulfillmentapp.Product, 0)
	for page := 1; page <= maxProductPages; page++ { // page 是当前商品页码。
		// payload、listErr 是当前页响应和请求错误。
		payload, listErr := client.fetchProductPage(ctx, instance, map[string]any{"page": strconv.Itoa(page), "pageSize": strconv.Itoa(productPageSize)})
		if listErr != nil {
			return nil, listErr
		}
		for _, item := range payload.Items { // item 是当前远程商品。
			products = append(products, item.product())
		}
		if len(payload.Items) < productPageSize || (payload.Count > 0 && page*productPageSize >= payload.Count) {
			return products, nil
		}
	}
	return nil, errors.New("蜜蜂汇云商品页数超过安全上限")
}

// GetProduct 按 miniunit_id 校验商品并生成直充账号字段。
func (client *Client) GetProduct(ctx context.Context, instance fulfillmentapp.Instance, goodsID int64) (fulfillmentapp.Product, error) {
	// product、productErr 是按 miniunit_id 读取的商品和错误。
	product, productErr := client.getRemoteProduct(ctx, instance, goodsID)
	if productErr != nil {
		return fulfillmentapp.Product{}, productErr
	}
	return product.product(), nil
}

// Buy 使用稳定 third_id 创建蜜蜂采购单，重复单号只查原单。
func (client *Client) Buy(ctx context.Context, instance fulfillmentapp.Instance, request fulfillmentapp.PurchaseRequest) (fulfillmentapp.RemoteOrder, error) {
	if request.Quantity <= 0 || request.Quantity > maximumPurchaseCount {
		return fulfillmentapp.RemoteOrder{}, fmt.Errorf("蜜蜂汇云单次采购数量必须在 1-%d 之间", maximumPurchaseCount)
	}
	// product、productErr 是下单前重新校验的商品和错误。
	product, productErr := client.getRemoteProduct(ctx, instance, request.RemoteGoodsID)
	if productErr != nil {
		return fulfillmentapp.RemoteOrder{}, fmt.Errorf("蜜蜂汇云下单前校验商品: %w", productErr)
	}
	if !product.canBuy() {
		return fulfillmentapp.RemoteOrder{}, errors.New("蜜蜂汇云商品当前不可采购")
	}
	// datas、datasErr 是按业务类型生成的放单参数和错误。
	datas, datasErr := purchaseDatas(product.BusinessID, request)
	if datasErr != nil {
		return fulfillmentapp.RemoteOrder{}, datasErr
	}
	// body 是不含公共签名字段的放单正文。
	body := map[string]any{
		"miniunit_id":   strconv.FormatInt(request.RemoteGoodsID, 10),
		"third_id":      strings.TrimSpace(request.ExternalOrderNo),
		"call_back_url": strings.TrimSpace(request.CallbackURL),
		"datas":         datas,
	}
	// data、callErr 是放单响应 data 和请求错误。
	data, callErr := client.call(ctx, instance, "/api/merchant/upload_order", body)
	if callErr != nil {
		// business 用于区分远程明确拒绝和结果不确定。
		var business *businessError
		if errors.As(callErr, &business) && business.Code == 10010 {
			return client.queryOrderList(ctx, instance, request.ExternalOrderNo)
		}
		if !errors.As(callErr, &business) {
			return fulfillmentapp.RemoteOrder{}, fmt.Errorf("%w: %v", fulfillmentapp.ErrSubmissionUncertain, callErr)
		}
		return fulfillmentapp.RemoteOrder{}, callErr
	}
	// payload 是放单成功后的订单数据。
	var payload orderPayload
	if decodeErr := json.Unmarshal(data, &payload); decodeErr != nil { // decodeErr 是放单 data 解码错误。
		return fulfillmentapp.RemoteOrder{}, fmt.Errorf("%w: 蜜蜂汇云下单 data 格式无效", fulfillmentapp.ErrSubmissionUncertain)
	}
	return payload.remoteOrder(request.ExternalOrderNo), nil
}

// QueryOrder 遵守蜜蜂首次 60 秒查单间隔和 5 分钟不存在宽限。
func (client *Client) QueryOrder(ctx context.Context, instance fulfillmentapp.Instance, query fulfillmentapp.OrderQuery) (fulfillmentapp.RemoteOrder, error) {
	// externalOrderNo 是稳定 third_id。
	externalOrderNo := strings.TrimSpace(query.ExternalOrderNo)
	// remoteOrderNo 是蜜蜂 order_id。
	remoteOrderNo := strings.TrimSpace(query.RemoteOrderNo)
	if externalOrderNo == "" && remoteOrderNo == "" {
		return fulfillmentapp.RemoteOrder{}, errors.New("查询蜜蜂汇云订单缺少外部订单号或远程订单号")
	}
	// createdAt、hasCreatedAt 是本地首次建单时间及有效性。
	createdAt, hasCreatedAt := parseCreatedAt(query.CreatedAt)
	if hasCreatedAt && client.Now().Sub(createdAt) < minimumQueryDelay {
		return waitingOrder(externalOrderNo, remoteOrderNo), nil
	}
	// body 是按远程单号或 third_id 二选一的查单正文。
	body := map[string]any{"order_id": remoteOrderNo, "third_id": "", "time": ""}
	if remoteOrderNo == "" {
		body["third_id"] = externalOrderNo
		if hasCreatedAt {
			body["time"] = strconv.FormatInt(createdAt.Unix(), 10)
		} else {
			return client.queryOrderList(ctx, instance, externalOrderNo)
		}
	}
	// data、callErr 是查单响应 data 和请求错误。
	data, callErr := client.call(ctx, instance, "/api/merchant/order_info", body)
	if callErr != nil {
		// business 用于识别 10015 订单不存在。
		var business *businessError
		if errors.As(callErr, &business) && business.Code == 10015 {
			if hasCreatedAt && client.Now().Sub(createdAt) < missingOrderGrace {
				return waitingOrder(externalOrderNo, remoteOrderNo), nil
			}
			return fulfillmentapp.RemoteOrder{}, fmt.Errorf("%w: 蜜蜂汇云订单不存在", fulfillmentapp.ErrNotFound)
		}
		return fulfillmentapp.RemoteOrder{}, callErr
	}
	// payload 是已验证成功的订单详情。
	var payload orderPayload
	if decodeErr := json.Unmarshal(data, &payload); decodeErr != nil { // decodeErr 是订单 data 解码错误。
		return fulfillmentapp.RemoteOrder{}, errors.New("蜜蜂汇云查单 data 格式无效")
	}
	return payload.remoteOrder(externalOrderNo), nil
}

// VerifyOrderCallback 第一版使用原单轮询，不接受未携带 HTTP 表单语义的通用回调。
func (client *Client) VerifyOrderCallback(_ fulfillmentapp.Instance, _ []byte) (fulfillmentapp.RemoteOrder, error) {
	return fulfillmentapp.RemoteOrder{}, errors.New("蜜蜂汇云当前使用原外部单号轮询查单")
}

// fetchProductPage 读取一页商品数据。
func (client *Client) fetchProductPage(ctx context.Context, instance fulfillmentapp.Instance, filters map[string]any) (productListPayload, error) {
	// body 是商品列表接口要求的完整筛选条件。
	body := map[string]any{"b_id": "", "product_id": "", "miniunit_id": "", "goods_name": "", "goods_sku": "", "pageSize": strconv.Itoa(productPageSize), "page": "1"}
	for key, value := range filters { // key、value 是调用方覆盖的筛选字段和值。
		body[key] = value
	}
	// data、callErr 是商品列表响应和请求错误。
	data, callErr := client.call(ctx, instance, "/api/merchant/productGoodsListNew", body)
	if callErr != nil {
		return productListPayload{}, callErr
	}
	// payload 是解码后的商品列表。
	var payload productListPayload
	if decodeErr := json.Unmarshal(data, &payload); decodeErr != nil { // decodeErr 是商品列表解码错误。
		return productListPayload{}, errors.New("蜜蜂汇云商品列表 data 格式无效")
	}
	return payload, nil
}

// getRemoteProduct 读取唯一商品，拒绝返回结果与 ID 不一致的响应。
func (client *Client) getRemoteProduct(ctx context.Context, instance fulfillmentapp.Instance, goodsID int64) (productPayload, error) {
	// payload、listErr 是按商品编号筛选的列表和请求错误。
	payload, listErr := client.fetchProductPage(ctx, instance, map[string]any{"miniunit_id": strconv.FormatInt(goodsID, 10)})
	if listErr != nil {
		return productPayload{}, listErr
	}
	for _, product := range payload.Items { // product 是当前候选商品。
		if int64Value(product.ID) == goodsID {
			return product, nil
		}
	}
	return productPayload{}, fulfillmentapp.ErrProductNotFound
}

// queryOrderList 在不知远程单号或重复 third_id 时只查原单，绝不创建新单。
func (client *Client) queryOrderList(ctx context.Context, instance fulfillmentapp.Instance, externalOrderNo string) (fulfillmentapp.RemoteOrder, error) {
	// now 是订单列表查询的当前时间。
	now := client.Now()
	// body 使用 third_id 和安全时间窗口筛选原订单。
	body := map[string]any{
		"order_id": "", "third_id": strings.TrimSpace(externalOrderNo), "biz_no": "", "status": "",
		"start_time": now.Add(-30 * 24 * time.Hour).Unix(), "end_time": now.Add(time.Minute).Unix(), "page": 1, "page_size": 50,
	}
	// data、callErr 是订单列表响应和请求错误。
	data, callErr := client.call(ctx, instance, "/api/merchant/order_list", body)
	if callErr != nil {
		return fulfillmentapp.RemoteOrder{}, callErr
	}
	// payload 是解码后的订单列表。
	var payload orderListPayload
	if decodeErr := json.Unmarshal(data, &payload); decodeErr != nil { // decodeErr 是订单列表解码错误。
		return fulfillmentapp.RemoteOrder{}, errors.New("蜜蜂汇云订单列表 data 格式无效")
	}
	for _, order := range payload.Items { // order 是当前候选远程订单。
		if strings.TrimSpace(order.ExternalOrderNo) == strings.TrimSpace(externalOrderNo) {
			return order.remoteOrder(externalOrderNo), nil
		}
	}
	// 查无结果时保持等待，避免因无法确认原放单时间而重复采购。
	return waitingOrder(externalOrderNo, ""), nil
}

// call 注入公共参数、按协议签名并解析统一响应。
func (client *Client) call(ctx context.Context, instance fulfillmentapp.Instance, path string, business map[string]any) (json.RawMessage, error) {
	// body 合并业务字段和协议公共字段。
	body := make(map[string]any, len(business)+3)
	for key, value := range business { // key、value 是待复制的业务字段和值。
		body[key] = value
	}
	body["app_key"] = strings.TrimSpace(instance.MerchantUserID)
	body["timestamp"] = strconv.FormatInt(client.Now().Unix(), 10)
	body["sign"] = requestSign(body, instance.APIKey)
	// encoded、encodeErr 是 JSON 请求体和编码错误。
	encoded, encodeErr := json.Marshal(body)
	if encodeErr != nil {
		return nil, encodeErr
	}
	// request、requestErr 是待发送的 HTTP 请求和创建错误。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(instance.BaseURL, "/")+path, bytes.NewReader(encoded))
	if requestErr != nil {
		return nil, fmt.Errorf("创建蜜蜂汇云请求: %w", requestErr)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	// response、responseErr 是 HTTP 响应和传输错误。
	response, responseErr := client.HTTPClient.Do(request)
	if responseErr != nil {
		return nil, fmt.Errorf("请求蜜蜂汇云失败: %w", responseErr)
	}
	defer response.Body.Close()
	// responseBody、readErr 是受限读取的响应体和读取错误。
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("读取蜜蜂汇云响应: %w", readErr)
	}
	if len(responseBody) > maxResponseBytes {
		return nil, errors.New("蜜蜂汇云响应过大")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("蜜蜂汇云返回 HTTP %d", response.StatusCode)
	}
	// envelope 是蜜蜂统一响应信封。
	var envelope responseEnvelope
	if decodeErr := json.Unmarshal(responseBody, &envelope); decodeErr != nil { // decodeErr 是统一响应解码错误。
		return nil, errors.New("蜜蜂汇云响应 JSON 无效")
	}
	if envelope.Code != 0 {
		// message 是去除空白后的业务错误信息。
		message := strings.TrimSpace(envelope.Message)
		if fulfillmentapp.IsSafePriceExceededMessage(message) || strings.Contains(strings.ToLower(message), "maxamount") {
			return nil, fmt.Errorf("%w: %s", fulfillmentapp.ErrSafePriceExceeded, message)
		}
		return nil, &businessError{Code: envelope.Code, Message: message}
	}
	return envelope.Data, nil
}

// requestSign 忽略 sign 和 datas，按 key 升序拼接 key+value+AppSecret 后计算小写 MD5。
func requestSign(parameters map[string]any, secret string) string {
	// keys 保存需要参与签名的参数名。
	keys := make([]string, 0, len(parameters))
	for key := range parameters { // key 是当前参数名。
		if key != "sign" && key != "datas" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	// builder 累积协议规定的签名明文。
	var builder strings.Builder
	for _, key := range keys { // key 是当前已排序参数名。
		builder.WriteString(key)
		builder.WriteString(textValue(parameters[key]))
	}
	builder.WriteString(secret)
	// sum 是签名明文的 MD5 摘要。
	sum := md5.Sum([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

// purchaseDatas 按蜜蜂业务类型组装不参与签名的 datas。
func purchaseDatas(businessID int, request fulfillmentapp.PurchaseRequest) (map[string]any, error) {
	// datas 是不参与签名的业务下单参数。
	datas := make(map[string]any, len(request.Attach)+2)
	for key, value := range request.Attach { // key、value 是规则配置的直充字段和值。
		if strings.TrimSpace(key) != "" {
			datas[key] = value
		}
	}
	if businessID != 13 && businessID != 18 && strings.TrimSpace(textValue(datas["target"])) == "" {
		return nil, errors.New("蜜蜂汇云直充商品缺少 datas.target 充值账号")
	}
	if businessID == 12 || businessID == 13 || businessID == 18 {
		datas["num"] = request.Quantity
	} else if request.Quantity != 1 {
		return nil, errors.New("当前蜜蜂直充商品一次只支持采购 1 份")
	}
	if request.SafePrice != "" && (businessID == 3 || businessID == 12 || businessID == 13 || businessID == 18) {
		if _, parseErr := strconv.ParseFloat(strings.TrimSpace(request.SafePrice), 64); parseErr != nil { // parseErr 是保护价数字校验错误。
			return nil, errors.New("蜜蜂汇云采购保护价必须是数字")
		}
		datas["maxAmount"] = json.Number(strings.TrimSpace(request.SafePrice))
	}
	return datas, nil
}

// responseEnvelope 描述蜜蜂接口统一响应外层。
type responseEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// businessError 保留蜜蜂业务错误码以便安全分支判断。
type businessError struct {
	Code    int
	Message string
}

// Error 返回可读的蜜蜂业务错误。
func (err *businessError) Error() string {
	if err.Message == "" {
		return fmt.Sprintf("蜜蜂汇云业务失败: code=%d", err.Code)
	}
	return fmt.Sprintf("蜜蜂汇云业务失败: %s (code=%d)", err.Message, err.Code)
}

// productListPayload 描述商品列表 data 节点。
type productListPayload struct {
	Items []productPayload `json:"data"`
	Count int              `json:"count"`
}

// productPayload 描述蜜蜂商品及其业务分类。
type productPayload struct {
	BusinessID int    `json:"b_id"`
	ID         any    `json:"miniunit_id"`
	GoodsSKU   string `json:"goods_sku"`
	GoodsName  string `json:"goods_name"`
	Spec       string `json:"spec"`
	Amount     any    `json:"amount"`
	Status     int    `json:"status"`
	Price      any    `json:"goods_price"`
}

// canBuy 判断商品状态和采购价是否允许采购。
func (payload productPayload) canBuy() bool {
	// price、priceErr 是转换后的采购价和解析错误。
	price, priceErr := strconv.ParseFloat(textValue(payload.Price), 64)
	return payload.Status == 1 && priceErr == nil && price > 0
}

// product 将蜜蜂商品转换为系统统一商品。
func (payload productPayload) product() fulfillmentapp.Product {
	// name 是组合商品名称和规格后的展示名称。
	name := strings.TrimSpace(strings.Join(nonEmpty(payload.GoodsName, payload.Spec), " | "))
	// goodsType 是系统统一的默认直充类型。
	goodsType := fulfillmentapp.GoodsTypeRecharge
	// attach 是直充商品默认要求收集的充值账号。
	attach := []fulfillmentapp.AttachField{{Type: "text", Name: "充值账号", Key: "target", Tip: "付款后从聊天中收集并确认"}}
	if payload.BusinessID == 6 {
		attach[0].Name = "充值手机号"
		attach[0].Validation = `^1\d{10}$`
	}
	if payload.BusinessID == 13 || payload.BusinessID == 18 {
		goodsType = fulfillmentapp.GoodsTypeCard
		attach = nil
	}
	// status 是映射到统一商品后的可售状态。
	status := payload.Status
	if !payload.canBuy() {
		status = 2
	}
	return fulfillmentapp.Product{ID: int64Value(payload.ID), Name: name, GoodsType: goodsType, FaceValue: textValue(payload.Amount), Price: textValue(payload.Price), Status: status, CanBuy: payload.canBuy(), Attach: attach}
}

// orderListPayload 描述蜜蜂订单列表 data 节点。
type orderListPayload struct {
	Items []orderPayload `json:"list"`
}

// orderPayload 描述蜜蜂订单状态和交付内容。
type orderPayload struct {
	State           int          `json:"state"`
	RemoteOrderNo   any          `json:"order_id"`
	ExternalOrderNo string       `json:"third_id"`
	Cost            any          `json:"cost"`
	Voucher         string       `json:"voucher"`
	Info            string       `json:"info"`
	ResponseInfo    string       `json:"rsp_info"`
	Cards           cardPayloads `json:"ys_cards"`
}

// remoteOrder 将蜜蜂订单转换为统一履约订单。
func (payload orderPayload) remoteOrder(fallbackExternal string) fulfillmentapp.RemoteOrder {
	// external 是蜜蜂回传或本地保留的稳定外部单号。
	external := strings.TrimSpace(payload.ExternalOrderNo)
	if external == "" {
		external = strings.TrimSpace(fallbackExternal)
	}
	// state 是映射后的统一履约状态。
	state := "unknown"
	switch payload.State {
	case 1, 11, 12:
		state = "waiting"
	case 2:
		state = "processing"
	case 3:
		state = "succeeded"
	case 4, 6:
		state = "failed"
	}
	// cards 是格式化后的卡号、卡密或兑换链接。
	cards := payload.Cards.texts()
	// rechargeInfo 是直充成功时发送给买家的结果信息。
	rechargeInfo := ""
	if state == "succeeded" && len(cards) == 0 {
		rechargeInfo = "充值成功"
		if strings.TrimSpace(payload.Voucher) != "" {
			rechargeInfo += "\n凭证：" + strings.TrimSpace(payload.Voucher)
		}
	}
	// hints 是供应商返回的履约补充信息。
	hints := strings.TrimSpace(payload.Info)
	if hints == "" {
		hints = strings.TrimSpace(payload.ResponseInfo)
	}
	return fulfillmentapp.RemoteOrder{RemoteOrderNo: textValue(payload.RemoteOrderNo), ExternalOrderNo: external, Status: payload.State, State: state, TotalPrice: textValue(payload.Cost), CardList: cards, RechargeInfo: rechargeInfo, RechargeHints: hints}
}

// cardPayloads 兼容蜜蜂返回单个卡券对象或卡券数组。
type cardPayloads []cardPayload

// UnmarshalJSON 解码单个或多个蜜蜂卡券。
func (cards *cardPayloads) UnmarshalJSON(data []byte) error {
	if string(data) == "null" || len(data) == 0 {
		return nil
	}
	// many 尝试按卡券数组解码。
	var many []cardPayload
	if err := json.Unmarshal(data, &many); err == nil { // err 是数组格式解码错误。
		*cards = many
		return nil
	}
	// one 在数组格式失败后尝试接收单个卡券。
	var one cardPayload
	if err := json.Unmarshal(data, &one); err != nil { // err 是单对象格式解码错误。
		return err
	}
	*cards = []cardPayload{one}
	return nil
}

// texts 将蜜蜂卡券转换为买家可读文本。
func (cards cardPayloads) texts() []string {
	// texts 保存全部非空卡券文本。
	texts := make([]string, 0, len(cards))
	for _, card := range cards { // card 是当前待格式化的卡券。
		// parts 保存当前卡券的账号和密码部分。
		parts := make([]string, 0, 2)
		if strings.TrimSpace(card.Number) != "" {
			// label 是卡号字段的展示标签。
			label := "卡号："
			if card.Type == 3 || card.Type == 5 {
				label = "兑换地址："
			}
			parts = append(parts, label+strings.TrimSpace(card.Number))
		}
		if strings.TrimSpace(card.Password) != "" {
			// label 是密码字段的展示标签。
			label := "卡密："
			if card.Type == 3 || card.Type == 4 {
				label = "兑换链接："
			} else if card.Type == 2 || card.Type == 5 {
				label = "兑换码："
			}
			parts = append(parts, label+strings.TrimSpace(card.Password))
		}
		if len(parts) > 0 {
			texts = append(texts, strings.Join(parts, "\n"))
		}
	}
	return texts
}

// cardPayload 描述蜜蜂返回的单张卡券。
type cardPayload struct {
	Number   string `json:"card_no"`
	Password string `json:"card_pwd"`
	Type     int    `json:"card_type"`
}

// waitingOrder 创建尚未到安全查询时间的统一等待订单。
func waitingOrder(externalOrderNo, remoteOrderNo string) fulfillmentapp.RemoteOrder {
	return fulfillmentapp.RemoteOrder{ExternalOrderNo: externalOrderNo, RemoteOrderNo: remoteOrderNo, Status: 1, State: "waiting"}
}

// parseCreatedAt 兼容数据库和 RFC3339 格式的本地建单时间。
func parseCreatedAt(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05"} { // layout 是当前候选时间格式。
		if parsed, err := time.Parse(layout, value); err == nil { // parsed、err 是解析结果和错误。
			return parsed, true
		}
	}
	return time.Time{}, false
}

// textValue 将 JSON 动态数字或文本稳定转换为字符串。
func textValue(value any) string {
	switch typed := value.(type) { // typed 是当前动态值的具体类型。
	case nil:
		return ""
	case string:
		return typed
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 32)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return fmt.Sprint(value)
	}
}

// int64Value 将动态商品或订单编号转换为整数。
func int64Value(value any) int64 {
	// parsed 是忽略无效输入后的整数结果。
	parsed, _ := strconv.ParseInt(strings.TrimSpace(textValue(value)), 10, 64)
	return parsed
}

// nonEmpty 返回去除首尾空白后的非空字符串。
func nonEmpty(values ...string) []string {
	// result 保存过滤后的字符串。
	result := make([]string, 0, len(values))
	for _, value := range values { // value 是当前候选字符串。
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}
