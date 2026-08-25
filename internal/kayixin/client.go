// Package kayixin 实现卡易信商家客户 API 3.0 协议。
package kayixin

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
	// protocolVersion 是卡易信请求头要求的固定 API 版本。
	protocolVersion = "3.0"
	// defaultTimeout 限制一次卡易信 HTTP 请求的总时间。
	defaultTimeout = 15 * time.Second
	// maxResponseBytes 限制远程响应体，避免异常站点消耗过多内存。
	maxResponseBytes = 2 << 20
	// maxProductPages 限制一次商品同步最多读取的页数。
	maxProductPages = 100
)

// Clock 提供可测试的秒级签名时间。
type Clock func() time.Time

// Client 使用卡易信 API 3.0 的请求头签名访问货源站。
type Client struct {
	// HTTPClient 是由组合根注入的受限出站客户端。
	HTTPClient *http.Client
	// Now 生成签名所需的十位秒级时间戳。
	Now Clock
}

// NewClient 创建卡易信 API 3.0 客户端。
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{HTTPClient: httpClient, Now: time.Now}
}

// ListProducts 分页读取卡易信全部商品摘要。
func (client *Client) ListProducts(ctx context.Context, instance fulfillmentapp.Instance) ([]fulfillmentapp.Product, error) {
	// products 保存逐页归一后的商品结果。
	products := make([]fulfillmentapp.Product, 0)
	for page := 1; page <= maxProductPages; page++ { // page 是当前请求的卡易信商品页码。
		// payload 是当前页的商品列表包装。
		var payload productListPayload
		// body 是卡易信商品列表接口要求的完整筛选条件。
		body := productListRequest{Page: page}
		if callErr := client.call(ctx, instance, "/api/v3/goods/getList", body, &payload); callErr != nil { // callErr 是当前页请求错误。
			return nil, callErr
		}
		for _, item := range payload.Items { // item 是当前页待归一化的远程商品。
			products = append(products, item.product(false))
		}
		// allPages 是远程声明的总页数，异常空值按单页处理。
		allPages := intValue(payload.AllPage)
		if allPages <= 1 || page >= allPages {
			return products, nil
		}
	}
	return nil, errors.New("卡易信商品页数超过安全上限")
}

// GetProduct 读取单个商品详情并转换直充模板。
func (client *Client) GetProduct(ctx context.Context, instance fulfillmentapp.Instance, goodsID int64) (fulfillmentapp.Product, error) {
	// payload 是卡易信商品详情响应。
	var payload productPayload
	if callErr := client.call(ctx, instance, "/api/v3/goods/getDetail", goodsDetailRequest{GoodsID: goodsID}, &payload); callErr != nil { // callErr 是商品详情请求错误。
		return fulfillmentapp.Product{}, callErr
	}
	if intValue(payload.SKUType) != 0 {
		return fulfillmentapp.Product{}, errors.New("卡易信多规格商品暂不能直接采购，请选择单规格商品")
	}
	return payload.product(true), nil
}

// Buy 使用稳定外部单号创建卡易信采购单。
func (client *Client) Buy(ctx context.Context, instance fulfillmentapp.Instance, request fulfillmentapp.PurchaseRequest) (fulfillmentapp.RemoteOrder, error) {
	// attachNames 按名称排序，确保数组正文和签名在重试时稳定一致。
	attachNames := make([]string, 0, len(request.Attach))
	for name := range request.Attach { // name 是待提交的卡易信直充字段名称。
		attachNames = append(attachNames, name)
	}
	sort.Strings(attachNames)
	// attach 是卡易信要求的名称和值对象数组。
	attach := make([]orderAttach, 0, len(attachNames))
	for _, name := range attachNames { // name 是排序后的直充字段名称。
		attach = append(attach, orderAttach{Name: name, Value: request.Attach[name]})
	}
	// body 是下单签名和发送共同使用的请求对象。
	body := createOrderRequest{GoodsID: strconv.FormatInt(request.RemoteGoodsID, 10), Count: request.Quantity, OuterNumber: request.ExternalOrderNo, Attach: attach}
	if strings.TrimSpace(request.SafePrice) != "" {
		// safePrice 是经过数字格式校验的订单总保护价。
		safePrice := json.Number(strings.TrimSpace(request.SafePrice))
		if _, parseErr := strconv.ParseFloat(safePrice.String(), 64); parseErr != nil { // parseErr 表示保护价不是合法数字。
			return fulfillmentapp.RemoteOrder{}, errors.New("卡易信采购保护价必须是数字")
		}
		body.SafePrice = &safePrice
	}
	// payload 是卡易信创建订单响应。
	var payload createOrderPayload
	if callErr := client.call(ctx, instance, "/api/v3/order/create", body, &payload); callErr != nil { // callErr 是远程下单请求错误。
		return fulfillmentapp.RemoteOrder{}, callErr
	}
	// status、state 表示创建成功后等待处理，立即返回卡密时直接视为完成。
	status, state := 1, "waiting"
	if len(payload.Cards) > 0 {
		status, state = 3, "succeeded"
	}
	return fulfillmentapp.RemoteOrder{RemoteOrderNo: textValue(payload.OrderNumber), ExternalOrderNo: request.ExternalOrderNo, Status: status, State: state, CardList: cardsText(payload.Cards)}, nil
}

// QueryOrder 优先按本地稳定外部单号查询卡易信订单。
func (client *Client) QueryOrder(ctx context.Context, instance fulfillmentapp.Instance, externalOrderNo, remoteOrderNo string) (fulfillmentapp.RemoteOrder, error) {
	// body 是卡易信支持按远程单号或外部单号二选一的查询参数。
	body := orderDetailRequest{OuterNumber: strings.TrimSpace(externalOrderNo)}
	if body.OuterNumber == "" {
		body.OrderNumber = strings.TrimSpace(remoteOrderNo)
	}
	if body.OuterNumber == "" && body.OrderNumber == "" {
		return fulfillmentapp.RemoteOrder{}, errors.New("查询卡易信订单缺少外部订单号或远程订单号")
	}
	// payload 是卡易信订单详情。
	var payload orderPayload
	if callErr := client.call(ctx, instance, "/api/v3/order/getDetail", body, &payload); callErr != nil { // callErr 是远程查单错误。
		return fulfillmentapp.RemoteOrder{}, callErr
	}
	return payload.remoteOrder(), nil
}

// VerifyOrderCallback 明确拒绝缺少签名请求头的通用回调入口。
func (client *Client) VerifyOrderCallback(_ fulfillmentapp.Instance, _ []byte) (fulfillmentapp.RemoteOrder, error) {
	return fulfillmentapp.RemoteOrder{}, errors.New("卡易信回调验签依赖 HTTP 请求头，当前接入请使用原外部单号轮询查单")
}

// call 使用完全相同的 JSON 字节完成签名和发送。
func (client *Client) call(ctx context.Context, instance fulfillmentapp.Instance, path string, body any, output any) error {
	// canonical 是同时用于 MD5 签名和请求体的 UTF-8 JSON。
	canonical, err := json.Marshal(body)
	if err != nil {
		return err
	}
	// timestamp 是卡易信协议要求的十位 Unix 秒时间戳。
	timestamp := strconv.FormatInt(client.Now().Unix(), 10)
	// request 是带卡易信四个鉴权头的 JSON POST 请求。
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(instance.BaseURL, "/")+path, bytes.NewReader(canonical))
	if err != nil {
		return fmt.Errorf("创建卡易信请求: %w", err)
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("X-APP-ID", instance.MerchantUserID)
	request.Header.Set("X-Version", protocolVersion)
	request.Header.Set("X-Timestamp", timestamp)
	request.Header.Set("X-Signature", md5Hex(instance.MerchantUserID+instance.APIKey+protocolVersion+timestamp+string(canonical)))
	// response 是货源站 HTTP 响应，不写入日志。
	response, err := client.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("请求卡易信站失败: %w", err)
	}
	defer response.Body.Close()
	// responseBody 是限制长度后的响应内容，仅用于解码。
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("读取卡易信响应: %w", err)
	}
	if len(responseBody) > maxResponseBytes {
		return errors.New("卡易信响应过大")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("卡易信站返回 HTTP %d", response.StatusCode)
	}
	// envelope 是卡易信统一业务响应包装。
	var envelope responseEnvelope
	if decodeErr := json.Unmarshal(responseBody, &envelope); decodeErr != nil { // decodeErr 是统一响应 JSON 解码错误。
		return errors.New("卡易信响应 JSON 无效")
	}
	if intValue(envelope.Code) != 1000 {
		// message 是卡易信返回的业务错误文本。
		message := strings.TrimSpace(envelope.Message)
		if strings.Contains(message, "不存在") || strings.Contains(message, "未找到") {
			return fmt.Errorf("%w: %s", fulfillmentapp.ErrNotFound, message)
		}
		return fmt.Errorf("卡易信业务失败: %s", message)
	}
	if output == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if decodeErr := json.Unmarshal(envelope.Data, output); decodeErr != nil { // decodeErr 是业务 data 字段解码错误。
		return errors.New("卡易信响应 data 格式无效")
	}
	return nil
}

// responseEnvelope 是卡易信统一响应结构。
type responseEnvelope struct {
	// Code 是成功时为 1000 的业务码。
	Code any `json:"code"`
	// Message 是业务结果说明。
	Message string `json:"msg"`
	// Data 是接口各自定义的业务数据。
	Data json.RawMessage `json:"data"`
}

// productListRequest 是商品列表的筛选请求。
type productListRequest struct {
	// Page 是从一开始的商品页码。
	Page int `json:"page"`
	// GoodsType 是空值时不限制商品类型。
	GoodsType string `json:"goodsType"`
	// Keyword 是空值时不按名称或编号搜索。
	Keyword string `json:"keyWord"`
	// SKUType 是空值时不限制规格类型。
	SKUType string `json:"skuType"`
	// ShowDirID 是空值时不限制展示分类。
	ShowDirID string `json:"showDirId"`
	// DirID 是空值时不限制商品分类。
	DirID string `json:"dirId"`
	// BrandID 是空值时不限制品牌。
	BrandID string `json:"brandId"`
}

// productListPayload 是卡易信分页商品数据。
type productListPayload struct {
	// AllPage 是远程声明的总页数。
	AllPage any `json:"allPage"`
	// Items 是当前页商品列表。
	Items []productPayload `json:"items"`
}

// goodsDetailRequest 是商品详情请求。
type goodsDetailRequest struct {
	// GoodsID 是卡易信商品主键。
	GoodsID int64 `json:"goodsId"`
}

// productPayload 是列表和详情共用的卡易信商品字段。
type productPayload struct {
	// GoodsID 是卡易信商品主键。
	GoodsID any `json:"goodsId"`
	// Name 是商品展示名称。
	Name string `json:"name"`
	// ImageURL 是商品图片地址。
	ImageURL string `json:"imgUrl"`
	// GoodsType 中 1 表示卡密，3 表示直充。
	GoodsType any `json:"goodsType"`
	// FaceValue 是商品面值。
	FaceValue any `json:"faceValue"`
	// SalesPrice 是当前采购单价。
	SalesPrice any `json:"salesPrice"`
	// Status 中 1 表示正在销售。
	Status any `json:"status"`
	// StockCount 是详情接口返回的可售库存。
	StockCount any `json:"stockCount"`
	// SKUType 中 0 表示当前系统可直接采购的单规格商品。
	SKUType any `json:"skuType"`
	// RechargeTemplates 是直充商品要求的动态字段。
	RechargeTemplates []rechargeTemplate `json:"rechargeTemplates"`
}

// rechargeTemplate 是卡易信直充字段定义。
type rechargeTemplate struct {
	// Type 是卡易信定义的控件数字类型。
	Type any `json:"type"`
	// Title 同时作为下单 attach 的字段名称。
	Title string `json:"title"`
	// Placeholder 是输入提示。
	Placeholder string `json:"placeholder"`
	// Required 中 1 表示必填。
	Required any `json:"required"`
	// Regex 是货源站提供的输入校验表达式。
	Regex string `json:"regex"`
	// Options 是下拉或级联字段的候选结构。
	Options any `json:"options"`
}

// product 把卡易信商品转换为应用层统一商品。
func (payload productPayload) product(detail bool) fulfillmentapp.Product {
	// goodsType 是卡易信类型到统一卡密或直充类型的映射。
	goodsType := fulfillmentapp.GoodsTypeCard
	if intValue(payload.GoodsType) == 3 {
		goodsType = fulfillmentapp.GoodsTypeRecharge
	}
	// attach 是转换后的直充字段列表。
	attach := make([]fulfillmentapp.AttachField, 0, len(payload.RechargeTemplates))
	for _, template := range payload.RechargeTemplates { // template 是当前卡易信直充字段。
		attach = append(attach, template.attachField())
	}
	// status、stock、singleSKU 是商品可售状态、库存和规格能力。
	status, stock, singleSKU := intValue(payload.Status), intValue(payload.StockCount), intValue(payload.SKUType) == 0
	// canBuy 在列表无库存字段时只判断销售和规格，详情同时要求正库存。
	canBuy := status == 1 && singleSKU
	if detail {
		canBuy = canBuy && stock > 0
	}
	return fulfillmentapp.Product{ID: int64(intValue(payload.GoodsID)), Name: payload.Name, Image: payload.ImageURL, GoodsType: goodsType, FaceValue: textValue(payload.FaceValue), Price: textValue(payload.SalesPrice), Status: status, Stock: stock, CanBuy: canBuy, Attach: attach}
}

// attachField 把卡易信字段类型和候选值转换为统一直充字段。
func (template rechargeTemplate) attachField() fulfillmentapp.AttachField {
	// fieldType 是前端可识别的统一输入控件类型。
	fieldType := "text"
	switch intValue(template.Type) {
	case 13:
		fieldType = "number"
	case 14, 16:
		fieldType = "select"
	case 15:
		fieldType = "textarea"
	case 1:
		fieldType = "image"
	}
	// tip 合并输入提示和必填约束，避免统一模型丢失必要信息。
	tip := strings.TrimSpace(template.Placeholder)
	if intValue(template.Required) == 1 && !strings.Contains(tip, "必填") {
		tip = strings.TrimSpace("必填 " + tip)
	}
	return fulfillmentapp.AttachField{Type: fieldType, Name: template.Title, Key: template.Title, Validation: template.Regex, Tip: tip, Options: fulfillmentapp.StringList(optionTexts(template.Options))}
}

// createOrderRequest 是卡易信创建订单请求。
type createOrderRequest struct {
	// GoodsID 是字符串形式的远程商品主键。
	GoodsID string `json:"goodsId"`
	// Count 是采购数量。
	Count int `json:"count"`
	// NotifyURL 留空表示由本系统按原外部单号轮询。
	NotifyURL string `json:"notifyUrl"`
	// OuterNumber 是本系统生成且重试不变的幂等单号。
	OuterNumber string `json:"outerNumber"`
	// SafePrice 是可选的订单总保护价。
	SafePrice *json.Number `json:"safePrice,omitempty"`
	// SKU 留空表示当前仅支持单规格商品。
	SKU string `json:"sku"`
	// Attach 是卡易信名称和值形式的直充字段。
	Attach []orderAttach `json:"attach"`
}

// orderAttach 是卡易信下单直充字段。
type orderAttach struct {
	// Name 必须与商品详情的充值模板标题一致。
	Name string `json:"name"`
	// Value 是从闲鱼聊天确认或规则固定值取得的真实充值内容。
	Value string `json:"value"`
}

// createOrderPayload 是创建订单成功后的数据。
type createOrderPayload struct {
	// OrderNumber 是卡易信远程订单号。
	OrderNumber any `json:"orderNumber"`
	// Cards 是卡密商品可能立即返回的明文卡券。
	Cards []cardPayload `json:"cards"`
}

// orderDetailRequest 是卡易信订单详情查询条件。
type orderDetailRequest struct {
	// OrderNumber 是可选的远程订单号。
	OrderNumber string `json:"orderNumber"`
	// OuterNumber 是优先使用的本地幂等单号。
	OuterNumber string `json:"outerNumber"`
}

// orderPayload 是卡易信订单详情字段。
type orderPayload struct {
	// OrderNumber 是卡易信远程订单号。
	OrderNumber any `json:"orderNumber"`
	// OuterNumber 是本系统提交的稳定外部单号。
	OuterNumber string `json:"outerNumber"`
	// Status 是卡易信订单数字状态。
	Status any `json:"status"`
	// Money 是订单实际采购金额。
	Money any `json:"money"`
	// Result 是直充结果或远程处理说明。
	Result string `json:"result"`
	// Cards 是查单接口返回的明文卡券。
	Cards []cardPayload `json:"cards"`
}

// remoteOrder 把卡易信订单详情转换为应用层统一结果。
func (payload orderPayload) remoteOrder() fulfillmentapp.RemoteOrder {
	// status 是卡易信原始订单状态。
	status := intValue(payload.Status)
	return fulfillmentapp.RemoteOrder{RemoteOrderNo: textValue(payload.OrderNumber), ExternalOrderNo: payload.OuterNumber, Status: status, State: orderState(status), TotalPrice: textValue(payload.Money), CardList: cardsText(payload.Cards), RechargeInfo: strings.TrimSpace(payload.Result)}
}

// cardPayload 是卡易信返回的卡券字段。
type cardPayload struct {
	// CardNumber 是可选卡号。
	CardNumber string `json:"cardNo"`
	// CardPassword 是可选卡密。
	CardPassword string `json:"cardPwd"`
}

// cardsText 把卡号和卡密整理成可直接发送给买家的文本。
func cardsText(cards []cardPayload) []string {
	// results 保存每张卡券的可读文本。
	results := make([]string, 0, len(cards))
	for _, card := range cards { // card 是当前待转换的卡易信卡券。
		// parts 保存当前卡券非空的卡号和卡密。
		parts := make([]string, 0, 2)
		if strings.TrimSpace(card.CardNumber) != "" {
			parts = append(parts, "卡号："+card.CardNumber)
		}
		if strings.TrimSpace(card.CardPassword) != "" {
			parts = append(parts, "卡密："+card.CardPassword)
		}
		if len(parts) > 0 {
			results = append(results, strings.Join(parts, "\n"))
		}
	}
	return results
}

// orderState 把卡易信数字状态转为本地稳定状态。
func orderState(status int) string {
	switch status {
	case 0:
		return "unpaid"
	case 1, 7:
		return "waiting"
	case 2, 8, 9:
		return "processing"
	case 3:
		return "succeeded"
	case 4:
		return "cancelled"
	case 5:
		return "refunded"
	default:
		return "unknown"
	}
}

// optionTexts 递归提取卡易信下拉和级联选项的显示值。
func optionTexts(value any) []string {
	// results 保存去重后的候选文本。
	results := make([]string, 0)
	// seen 防止级联结构中的重复名称进入统一选项。
	seen := map[string]struct{}{}
	// visit 遍历字符串、数组和嵌套对象候选值。
	var visit func(any)
	visit = func(current any) { // current 是当前待遍历的候选节点。
		switch typed := current.(type) { // typed 是保留当前候选节点实际 JSON 类型的分支值。
		case string:
			for _, item := range strings.FieldsFunc(typed, func(separator rune) bool { // separator 是候选文本中的常见分隔符。
				return separator == ',' || separator == '，' || separator == '|' || separator == '\n'
			}) {
				// normalized 是去除空白后的候选文本。
				normalized := strings.TrimSpace(item)
				if normalized != "" {
					if _, exists := seen[normalized]; !exists { // exists 表示候选文本是否已经加入结果。
						seen[normalized] = struct{}{}
						results = append(results, normalized)
					}
				}
			}
		case []any:
			for _, child := range typed { // child 是数组中的下一个候选节点。
				visit(child)
			}
		case map[string]any:
			// label 优先使用名称，其次使用实际值。
			label := textValue(typed["name"])
			if label == "" {
				label = textValue(typed["label"])
			}
			if label == "" {
				label = textValue(typed["value"])
			}
			visit(label)
			visit(typed["children"])
		}
	}
	visit(value)
	return results
}

// textValue 把远程字符串或数字字段统一转成文本。
func textValue(value any) string {
	switch typed := value.(type) { // typed 是远程字段保留实际 JSON 类型后的分支值。
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

// intValue 把远程字符串或数字字段统一转成整数。
func intValue(value any) int {
	// parsed 是忽略无效值后的整数结果。
	parsed, _ := strconv.Atoi(strings.SplitN(textValue(value), ".", 2)[0])
	return parsed
}

// md5Hex 返回卡易信协议要求的小写 MD5 十六进制文本。
func md5Hex(value string) string {
	// digest 是待签名文本的 MD5 摘要。
	digest := md5.Sum([]byte(value))
	return hex.EncodeToString(digest[:])
}
