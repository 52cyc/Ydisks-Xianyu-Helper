package adapter

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	itemapp "xianyu-go/internal/application/items"
)

const (
	// maxItemSharePageBytes 限制淘宝短链落地页读取量，页面只需包含目标商品地址。
	maxItemSharePageBytes = 512 << 10
)

var (
	// itemShareURLPattern 从闲鱼复制文案或 Markdown 链接中提取 HTTP(S) 地址。
	itemShareURLPattern = regexp.MustCompile(`https?://[^\s\]\[<>()"']+`)
	// itemShareTargetPattern 只从短链页面声明的闲鱼官方目标地址提取商品标识，避免误用其他脚本参数。
	itemShareTargetPattern = regexp.MustCompile(`https?://(?:www\.|h5\.m\.)?goofish\.com/[^\s'"<>]*?(?:[?&])id=(\d{6,24})(?:[&#'"\\]|$)`)
	// plainItemIDPattern 允许运营人员直接粘贴六到二十四位闲鱼商品标识。
	plainItemIDPattern = regexp.MustCompile(`^\d{6,24}$`)
)

// itemLinkHTTPDoer 定义短链解析只需要的 HTTP 请求能力，便于本地测试隔离公网。
type itemLinkHTTPDoer interface {
	// Do 执行受 Context 控制且带出站安全策略的请求。
	Do(*http.Request) (*http.Response, error)
}

// ItemLinkResolver 将闲鱼或淘宝分享文本安全解析为标准闲鱼商品地址。
type ItemLinkResolver struct {
	// client 是组合根注入的限时、限响应和受控重定向 HTTP 客户端。
	client itemLinkHTTPDoer
}

// NewItemLinkResolver 创建分享链接解析器，缺少安全 HTTP 客户端时拒绝构造。
func NewItemLinkResolver(client itemLinkHTTPDoer) (*ItemLinkResolver, error) {
	if client == nil {
		return nil, errors.New("商品分享链接 HTTP 客户端不能为空")
	}
	return &ItemLinkResolver{client: client}, nil
}

// Resolve 从纯商品 ID、标准闲鱼地址或淘宝短链分享文案中提取商品 ID，并移除分享跟踪参数。
func (resolver *ItemLinkResolver) Resolve(ctx context.Context, rawSource string) (itemapp.LinkImportResolvedSource, error) {
	if resolver == nil || resolver.client == nil {
		return itemapp.LinkImportResolvedSource{}, errors.New("商品分享链接解析器未初始化")
	}
	// source 是去除首尾空白后的用户分享文本。
	source := strings.TrimSpace(rawSource)
	if plainItemIDPattern.MatchString(source) {
		return resolvedItemSource(source), nil
	}
	// rawURL 是分享文案中第一个 HTTP(S) 地址；同一条输入不允许隐式尝试多个外部目标。
	rawURL := firstItemShareURL(source)
	if rawURL == "" {
		return itemapp.LinkImportResolvedSource{}, errors.New("未找到可识别的闲鱼分享链接")
	}
	// parsed 和 parseErr 保存分享地址结构及解析错误。
	parsed, parseErr := url.Parse(rawURL)
	if parseErr != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return itemapp.LinkImportResolvedSource{}, errors.New("闲鱼分享链接格式无效")
	}
	// host 是统一小写并移除尾部点号的分享域名。
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if !isAllowedItemShareHost(host) {
		return itemapp.LinkImportResolvedSource{}, errors.New("仅支持闲鱼或淘宝官方分享链接")
	}
	if // itemID 是标准详情地址直接携带的商品标识。
	itemID := strings.TrimSpace(parsed.Query().Get("id")); plainItemIDPattern.MatchString(itemID) {
		return resolvedItemSource(itemID), nil
	}
	if host != "m.tb.cn" {
		return itemapp.LinkImportResolvedSource{}, errors.New("闲鱼链接缺少有效商品 ID")
	}
	return resolver.resolveShortLink(ctx, parsed.String())
}

// resolveShortLink 读取淘宝官方短链落地页并提取其中声明的闲鱼目标商品 ID。
func (resolver *ItemLinkResolver) resolveShortLink(ctx context.Context, shortURL string) (itemapp.LinkImportResolvedSource, error) {
	// request 和 requestErr 保存带调用方取消能力的短链请求及构造错误。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, shortURL, nil)
	if requestErr != nil {
		return itemapp.LinkImportResolvedSource{}, errors.New("淘宝短链格式无效")
	}
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Ydisks-Xianyu-Helper/1.0)")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	// response 和 responseErr 保存受策略客户端限制的短链响应及网络错误。
	response, responseErr := resolver.client.Do(request)
	if responseErr != nil {
		return itemapp.LinkImportResolvedSource{}, fmt.Errorf("解析淘宝短链失败: %w", responseErr)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return itemapp.LinkImportResolvedSource{}, fmt.Errorf("解析淘宝短链失败: HTTP %d", response.StatusCode)
	}
	if response.Request != nil && response.Request.URL != nil && !isAllowedItemShareHost(strings.TrimSuffix(strings.ToLower(response.Request.URL.Hostname()), ".")) {
		return itemapp.LinkImportResolvedSource{}, errors.New("淘宝短链跳转到了不受支持的站点")
	}
	// body 和 readErr 保存带一字节超限探测的短链页面内容。
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxItemSharePageBytes+1))
	if readErr != nil {
		return itemapp.LinkImportResolvedSource{}, errors.New("读取淘宝短链页面失败")
	}
	if len(body) > maxItemSharePageBytes {
		return itemapp.LinkImportResolvedSource{}, errors.New("淘宝短链页面内容过大")
	}
	// decoded 是还原 HTML 实体后的页面文本，便于同时匹配普通参数和 &amp; 参数。
	decoded := html.UnescapeString(string(body))
	// matches 保存目标地址中的商品 ID 捕获组。
	matches := itemShareTargetPattern.FindStringSubmatch(decoded)
	if len(matches) != 2 || !plainItemIDPattern.MatchString(matches[1]) {
		return itemapp.LinkImportResolvedSource{}, errors.New("淘宝短链中未找到闲鱼商品 ID，链接可能已失效")
	}
	return resolvedItemSource(matches[1]), nil
}

// firstItemShareURL 返回分享文本中的第一个地址，并移除中文或英文句末标点。
func firstItemShareURL(source string) string {
	// matched 是正则从用户分享文本中提取的首个候选地址。
	matched := itemShareURLPattern.FindString(source)
	return strings.TrimRight(matched, "，。；;！!、）】}")
}

// isAllowedItemShareHost 判断地址是否属于当前支持的闲鱼详情或淘宝短链官方域名。
func isAllowedItemShareHost(host string) bool {
	switch host {
	case "m.tb.cn", "www.goofish.com", "goofish.com", "h5.m.goofish.com":
		return true
	default:
		return false
	}
}

// resolvedItemSource 创建不包含分享参数和用户归因信息的标准商品定位。
func resolvedItemSource(itemID string) itemapp.LinkImportResolvedSource {
	// normalizedID 是通过数字格式检查后的商品标识。
	normalizedID := strings.TrimSpace(itemID)
	return itemapp.LinkImportResolvedSource{ItemID: normalizedID, ItemURL: "https://www.goofish.com/item?id=" + url.QueryEscape(normalizedID)}
}

// 编译期保证链接解析器实现应用层消费者定义的最小接口。
var _ itemapp.LinkImportResolverPort = (*ItemLinkResolver)(nil)
