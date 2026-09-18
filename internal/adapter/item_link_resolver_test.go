package adapter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// itemLinkRoundTripperFunc 把函数适配为短链解析测试使用的 HTTP 传输。
type itemLinkRoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip 执行测试注入的确定性短链响应函数。
func (transport itemLinkRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// TestItemLinkResolverDirectAndSharedText 验证标准地址、完整分享文案和纯商品 ID 均收敛为无跟踪参数地址。
func TestItemLinkResolverDirectAndSharedText(t *testing.T) {
	// resolver 是不会实际访问网络的链接解析器。
	resolver, resolverErr := NewItemLinkResolver(&http.Client{Transport: itemLinkRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("direct links must not use network")
	})})
	if resolverErr != nil {
		t.Fatalf("NewItemLinkResolver() error = %v", resolverErr)
	}
	// cases 保存受支持直接输入及其商品标识。
	cases := []struct {
		// source 是运营人员可能粘贴的输入。
		source string
		// itemID 是期望解析出的商品标识。
		itemID string
	}{
		{source: "1073801913752", itemID: "1073801913752"},
		{source: "【闲鱼】[https://www.goofish.com/item?id=1073801913752&spm=share](https://www.goofish.com/item?id=1073801913752) 点击打开", itemID: "1073801913752"},
		{source: "https://h5.m.goofish.com/item?foo=1&id=99887766。", itemID: "99887766"},
	}
	// testCase 表示当前直接解析子场景。
	for _, testCase := range cases {
		// resolved 和 resolveErr 保存当前输入的标准化结果。
		resolved, resolveErr := resolver.Resolve(context.Background(), testCase.source)
		if resolveErr != nil || resolved.ItemID != testCase.itemID || resolved.ItemURL != "https://www.goofish.com/item?id="+testCase.itemID {
			t.Fatalf("Resolve(%q) = %+v, %v", testCase.source, resolved, resolveErr)
		}
	}
}

// TestItemLinkResolverShortLink 验证淘宝短链页面中的 JavaScript 目标地址可以安全提取商品 ID。
func TestItemLinkResolverShortLink(t *testing.T) {
	// client 返回与真实淘宝短链结构一致的有限 HTML 页面。
	client := &http.Client{Transport: itemLinkRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Hostname() != "m.tb.cn" {
			t.Fatalf("unexpected host = %s", request.URL.Hostname())
		}
		// body 是包含 HTML 转义查询参数的短链落地页。
		body := `<script>var unrelated='https://example.com/callback?id=123456';var url='https://h5.m.goofish.com/item?forceFlush=1&amp;id=1073801913752&amp;spm=share';</script>`
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request, Header: make(http.Header)}, nil
	})}
	// resolver 是使用本地传输的短链解析器。
	resolver, resolverErr := NewItemLinkResolver(client)
	if resolverErr != nil {
		t.Fatalf("NewItemLinkResolver() error = %v", resolverErr)
	}
	// resolved 和 resolveErr 保存完整闲鱼分享文案解析结果。
	resolved, resolveErr := resolver.Resolve(context.Background(), "【闲鱼】https://m.tb.cn/h.8HT9qv7?tk=token CZ001 点击打开")
	if resolveErr != nil || resolved.ItemID != "1073801913752" {
		t.Fatalf("resolved = %+v, error = %v", resolved, resolveErr)
	}
}

// TestItemLinkResolverRejectsUnsafeAndInvalidInputs 验证第三方域名、无商品 ID 和超限页面均失败。
func TestItemLinkResolverRejectsUnsafeAndInvalidInputs(t *testing.T) {
	// largeBody 创建超过短链页面限制的响应，验证读取边界。
	largeBody := strings.Repeat("x", maxItemSharePageBytes+1)
	// resolver 使用根据路径返回缺 ID 或超限页面的测试客户端。
	resolver, resolverErr := NewItemLinkResolver(&http.Client{Transport: itemLinkRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		// body 根据短链路径选择当前错误场景响应。
		body := "<html>missing item</html>"
		if strings.Contains(request.URL.Path, "large") {
			body = largeBody
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: request, Header: make(http.Header)}, nil
	})})
	if resolverErr != nil {
		t.Fatalf("NewItemLinkResolver() error = %v", resolverErr)
	}
	// sources 保存所有应被拒绝的输入。
	sources := []string{"https://example.com/item?id=1073801913752", "https://www.goofish.com/item", "https://m.tb.cn/missing", "https://m.tb.cn/large"}
	// source 表示当前待拒绝的用户输入。
	for _, source := range sources {
		if // resolveErr 是当前非法输入返回的安全错误。
		_, resolveErr := resolver.Resolve(context.Background(), source); resolveErr == nil {
			t.Fatalf("Resolve(%q) should fail", source)
		}
	}
	if _, err := NewItemLinkResolver(nil); err == nil {
		t.Fatal("nil client should fail")
	}
}
