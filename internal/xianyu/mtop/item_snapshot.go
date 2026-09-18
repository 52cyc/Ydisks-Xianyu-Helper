package mtop

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// ItemSnapshotFetcher 是外链采集可选的平台详情能力，不扩大通用 Client 测试替身的必需方法集合。
type ItemSnapshotFetcher interface {
	// FetchItemSnapshot 使用请求作用域 Cookie 读取公开商品字段；返回值不包含卖家身份或凭证明文。
	FetchItemSnapshot(context.Context, string, string) (ItemSnapshot, error)
}

// ItemSnapshot 是闲鱼详情响应收敛后的非敏感、可进入批量预检的商品快照。
type ItemSnapshot struct {
	// Title 是平台详情提供的商品标题。
	Title string
	// Description 是平台详情提供的商品描述，字段缺失时允许上层回退为标题。
	Description string
	// PriceText 是不带货币符号的十进制元金额文本。
	PriceText string
	// ImageURLs 是源商品按顺序提供的最多九张 HTTP(S) 图片。
	ImageURLs []string
	// IsMultiSpec 表示详情中存在多规格标记、规格维度或多个 SKU。
	IsMultiSpec bool
}

// FetchItemSnapshot 调用既有商品详情协议并提取发布预检所需字段，缺少标题、价格或图片时返回安全错误。
func (c *ClientImpl) FetchItemSnapshot(ctx context.Context, cookies, itemID string) (ItemSnapshot, error) {
	// detail 和 detailErr 保存经过 Token 刷新重试后的平台详情事实。
	detail, detailErr := c.fetchItemDetail(ctx, cookies, itemID)
	if detailErr != nil {
		return ItemSnapshot{}, detailErr
	}
	// snapshot 是从受控详情根节点提取的非敏感商品快照。
	snapshot := itemSnapshotFromDetail(detail)
	if snapshot.Title == "" {
		return ItemSnapshot{}, errors.New("商品详情缺少标题，可能已下架或不可见")
	}
	if snapshot.PriceText == "" {
		return ItemSnapshot{}, errors.New("商品详情缺少有效价格")
	}
	if len(snapshot.ImageURLs) == 0 {
		return ItemSnapshot{}, errors.New("商品详情缺少可用图片")
	}
	return snapshot, nil
}

// itemSnapshotFromDetail 只遍历当前商品的已知详情节点，避免误采集推荐商品中的标题、价格或图片。
func itemSnapshotFromDetail(detail map[string]any) ItemSnapshot {
	// roots 保存按优先级排列的当前商品详情节点，顶层字段优先于兼容嵌套结构。
	roots := itemSnapshotRoots(detail)
	// snapshot 保存逐字段首次命中的归一化结果，规格探测只检查当前商品的已知字段。
	snapshot := ItemSnapshot{IsMultiSpec: snapshotHasMultiSpec(roots)}
	// root 表示当前待检查的商品详情兼容节点。
	for _, root := range roots {
		if snapshot.Title == "" {
			snapshot.Title = firstSnapshotString(root, "title", "itemTitle", "itemName")
			if // textDTO 是官方编辑详情中的标题和描述对象。
			textDTO := mapFromAny(root["itemTextDTO"]); snapshot.Title == "" {
				snapshot.Title = firstSnapshotString(textDTO, "title", "itemTitle")
			}
		}
		if snapshot.Description == "" {
			snapshot.Description = firstSnapshotString(root, "desc", "description", "itemDescription")
			if // textDTO 是官方编辑详情中的标题和描述对象。
			textDTO := mapFromAny(root["itemTextDTO"]); snapshot.Description == "" {
				snapshot.Description = firstSnapshotString(textDTO, "desc", "description")
			}
		}
		if snapshot.PriceText == "" {
			snapshot.PriceText = snapshotPrice(root)
		}
	}
	// seenImages 防止主图和图片列表包含同一地址时重复发布。
	seenImages := make(map[string]struct{}, 9)
	// root 表示当前待读取图片字段的商品详情兼容节点。
	for _, root := range roots {
		appendSnapshotImages(&snapshot.ImageURLs, seenImages, root)
		if len(snapshot.ImageURLs) >= 9 {
			break
		}
	}
	return snapshot
}

// snapshotHasMultiSpec 仅在当前商品兼容节点的规格字段内探测多规格，避免推荐商品造成误判。
func snapshotHasMultiSpec(roots []map[string]any) bool {
	// root 表示当前待检查的商品详情兼容节点。
	for _, root := range roots {
		// specFields 只保留已知的当前商品规格标记和 SKU 包装节点。
		specFields := make(map[string]any)
		// key 表示平台不同版本可能使用的规格字段名。
		for _, key := range []string{"multiSku", "isMultiSku", "isMultiSpec", "multipleSku", "skuList", "skus", "skuProps", "skuProperties", "specProps", "specifications", "skuDO", "skuBase", "skuModel"} {
			if // value 和 exists 表示当前兼容节点是否包含该规格字段。
			value, exists := root[key]; exists {
				specFields[key] = value
			}
		}
		if detectItemMultiSpec(specFields) {
			return true
		}
	}
	return false
}

// itemSnapshotRoots 返回当前商品的有限兼容节点，不递归搜索可能包含推荐商品的任意响应对象。
func itemSnapshotRoots(detail map[string]any) []map[string]any {
	// roots 从平台详情顶层开始，保证明确字段拥有最高优先级。
	roots := []map[string]any{detail}
	// key 表示平台不同版本用于包装当前商品详情的已知字段名。
	for _, key := range []string{"itemDO", "item", "itemInfo", "itemDetail", "baseData"} {
		if // node 和 ok 表示该兼容字段是否为对象。
		node, ok := detail[key].(map[string]any); ok {
			roots = append(roots, node)
		}
	}
	return roots
}

// firstSnapshotString 返回字段列表中首个非空平台文本，数字字段也通过 mtopString 安全转换。
func firstSnapshotString(values map[string]any, keys ...string) string {
	// key 表示当前候选字段名。
	for _, key := range keys {
		if // value 是去除首尾空白后的平台字段文本。
		value := strings.TrimSpace(mtopString(values[key])); value != "" {
			return value
		}
	}
	return ""
}

// snapshotPrice 兼容官方分金额对象和详情页元金额文本，并拒绝零值、负值及非数字内容。
func snapshotPrice(root map[string]any) string {
	// priceDTO 是官方详情中以分为单位的价格对象。
	priceDTO := mapFromAny(root["itemPriceDTO"])
	if // centsRaw 是平台成交价的分金额文本。
	centsRaw := firstSnapshotString(priceDTO, "priceInCent", "currentPriceInCent", "soldPriceInCent"); centsRaw != "" {
		if // cents 和 centsErr 是严格解析后的正分金额。
		cents, centsErr := strconv.ParseInt(centsRaw, 10, 64); centsErr == nil && cents > 0 {
			return centsText(cents)
		}
	}
	// rawPrice 是详情页可能直接提供的元金额文本。
	rawPrice := firstSnapshotString(root, "price", "priceText", "soldPrice", "currentPrice")
	if // priceMap 和 ok 表示价格字段是否使用嵌套对象。
	priceMap, ok := root["price"].(map[string]any); ok && rawPrice == "" {
		rawPrice = firstSnapshotString(priceMap, "value", "text", "price")
	}
	// normalized 去除货币符号和千位分隔符，保留批量发布接受的十进制金额。
	normalized := strings.TrimSpace(strings.NewReplacer("¥", "", "￥", "", ",", "").Replace(rawPrice))
	// price 和 priceErr 用于确认金额是大于零的有限数值。
	price, priceErr := strconv.ParseFloat(normalized, 64)
	if priceErr != nil || price <= 0 {
		return ""
	}
	return strconv.FormatFloat(price, 'f', 2, 64)
}

// appendSnapshotImages 从官方图片列表和常见主图字段追加有效地址，保持顺序并限制为九张。
func appendSnapshotImages(images *[]string, seen map[string]struct{}, root map[string]any) {
	// listKey 表示平台不同版本的图片数组字段。
	for _, listKey := range []string{"imageInfoDOList", "images", "imageList", "pics"} {
		// values 和 ok 表示当前图片字段是否为数组。
		values, ok := root[listKey].([]any)
		if !ok {
			continue
		}
		// value 表示当前图片数组元素，可以是 URL 字符串或包含 URL 的对象。
		for _, value := range values {
			// imageURL 保存当前图片元素解析出的地址。
			imageURL := strings.TrimSpace(mtopString(value))
			if // imageMap 和 imageObject 表示当前元素是否为图片对象。
			imageMap, imageObject := value.(map[string]any); imageObject {
				imageURL = firstSnapshotString(imageMap, "url", "imageUrl", "imageURL", "picUrl", "picURL")
			}
			appendSnapshotImage(images, seen, imageURL)
			if len(*images) >= 9 {
				return
			}
		}
	}
	// imageKey 表示平台常见的单张主图字段。
	for _, imageKey := range []string{"picUrl", "picURL", "mainPic", "mainImage", "imageUrl", "imageURL"} {
		appendSnapshotImage(images, seen, firstSnapshotString(root, imageKey))
		if len(*images) >= 9 {
			return
		}
	}
}

// appendSnapshotImage 校验图片协议和主机并去重，发布阶段仍会再次执行公网与媒体内容检查。
func appendSnapshotImage(images *[]string, seen map[string]struct{}, rawURL string) {
	// parsed 和 parseErr 保存图片地址结构及解析错误。
	parsed, parseErr := url.Parse(strings.TrimSpace(rawURL))
	if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return
	}
	// normalized 是用于去重和后续远程下载的标准地址文本。
	normalized := parsed.String()
	if _, exists := seen[normalized]; exists {
		return
	}
	seen[normalized] = struct{}{}
	*images = append(*images, normalized)
}

// 编译期保证生产 MTOP 客户端实现外链商品快照能力。
var _ ItemSnapshotFetcher = (*ClientImpl)(nil)
