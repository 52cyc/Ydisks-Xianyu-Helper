package items

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// linkImportResolverFake 按分享文本返回预置商品标识，用于隔离短链网络访问。
type linkImportResolverFake struct {
	// failures 保存需要模拟解析失败的原始输入。
	failures map[string]error
}

// Resolve 返回输入末段作为商品标识，或返回测试预置的解析错误。
func (fake linkImportResolverFake) Resolve(_ context.Context, source string) (LinkImportResolvedSource, error) {
	if // err 和 failed 表示当前输入是否应模拟解析失败。
	err, failed := fake.failures[source]; failed {
		return LinkImportResolvedSource{}, err
	}
	// parts 保存按冒号切分后的测试输入片段。
	parts := strings.Split(source, ":")
	// itemID 是测试输入最后一段承载的商品标识。
	itemID := parts[len(parts)-1]
	return LinkImportResolvedSource{ItemID: itemID, ItemURL: "https://www.goofish.com/item?id=" + itemID}, nil
}

// linkImportCollectorFake 按商品标识返回预置详情或错误，并记录调用顺序。
type linkImportCollectorFake struct {
	// items 保存每个测试商品的采集快照。
	items map[string]LinkImportCollectedItem
	// failures 保存需要模拟平台详情失败的商品。
	failures map[string]error
	// calls 保存实际进入详情采集的商品标识顺序。
	calls []string
}

// Collect 记录调用并返回预置详情，供服务测试验证重复项不会再次访问平台。
func (fake *linkImportCollectorFake) Collect(_ context.Context, userID int64, cookieID, itemID string) (LinkImportCollectedItem, error) {
	if userID != 7 || cookieID != "account-1" {
		return LinkImportCollectedItem{}, errors.New("测试账号范围错误")
	}
	fake.calls = append(fake.calls, itemID)
	if // err 和 failed 表示当前商品是否模拟详情失败。
	err, failed := fake.failures[itemID]; failed {
		return LinkImportCollectedItem{}, err
	}
	return fake.items[itemID], nil
}

// TestLinkImportServiceCollectBatch 验证批量采集保留成功项、隔离单条失败并拒绝重复和多规格。
func TestLinkImportServiceCollectBatch(t *testing.T) {
	// collector 保存成功、平台失败和多规格三类测试快照。
	collector := &linkImportCollectorFake{
		items: map[string]LinkImportCollectedItem{
			"100": {Title: "商品一", Price: "9.90", Images: []string{"https://img.example/1.jpg"}},
			"300": {Title: "多规格", Price: "19.90", Images: []string{"https://img.example/3.jpg"}, IsMultiSpec: true},
		},
		failures: map[string]error{"200": errors.New("商品已下架")},
	}
	// service 是使用确定性替身构造的批量采集服务。
	service, serviceErr := NewLinkImportService(linkImportResolverFake{failures: map[string]error{"bad": errors.New("链接格式不受支持")}}, collector)
	if serviceErr != nil {
		t.Fatalf("NewLinkImportService() error = %v", serviceErr)
	}
	// result 和 collectErr 保存混合批次执行结果。
	result, collectErr := service.CollectBatch(context.Background(), LinkImportInput{UserID: 7, CookieID: " account-1 ", Sources: []string{"item:100", "", "bad", "item:200", "item:100", "item:300"}})
	if collectErr != nil {
		t.Fatalf("CollectBatch() error = %v", collectErr)
	}
	if result.Total != 5 || result.Collected != 1 || result.Failed != 4 || len(result.Rows) != 5 {
		t.Fatalf("unexpected result = %+v", result)
	}
	if result.Rows[0].Description != "商品一" || result.Rows[0].ItemID != "100" {
		t.Fatalf("success row = %+v", result.Rows[0])
	}
	if !strings.Contains(result.Rows[3].Error, "第 1 条") || !strings.Contains(result.Rows[4].Error, "多规格") {
		t.Fatalf("failure rows = %+v", result.Rows)
	}
	if strings.Join(collector.calls, ",") != "100,200,300" {
		t.Fatalf("collector calls = %v", collector.calls)
	}
}

// TestLinkImportServiceValidation 验证身份、账号、空输入、数量上限和构造依赖均在远端调用前失败。
func TestLinkImportServiceValidation(t *testing.T) {
	// collector 是不会被参数校验场景调用的平台替身。
	collector := &linkImportCollectorFake{}
	// service 是完整依赖的待测服务。
	service, serviceErr := NewLinkImportService(linkImportResolverFake{}, collector)
	if serviceErr != nil {
		t.Fatalf("NewLinkImportService() error = %v", serviceErr)
	}
	// tooMany 保存超过单批限制的分享文本集合。
	tooMany := make([]string, MaxLinkImportSources+1)
	// sourceIndex 表示当前填充的超限输入下标。
	for sourceIndex := range tooMany {
		tooMany[sourceIndex] = "item:1"
	}
	// cases 保存所有应返回顶层参数错误的输入和目标错误。
	cases := []struct {
		// name 是子测试名称。
		name string
		// input 是待验证的采集输入。
		input LinkImportInput
		// want 是应通过 errors.Is 匹配的稳定应用错误。
		want error
	}{
		{name: "user", input: LinkImportInput{CookieID: "a", Sources: []string{"item:1"}}, want: ErrLinkImportInvalidUser},
		{name: "account", input: LinkImportInput{UserID: 1, Sources: []string{"item:1"}}, want: ErrLinkImportInvalidAccount},
		{name: "sources", input: LinkImportInput{UserID: 1, CookieID: "a", Sources: []string{" "}}, want: ErrLinkImportNoSources},
		{name: "limit", input: LinkImportInput{UserID: 1, CookieID: "a", Sources: tooMany}, want: ErrLinkImportTooManySources},
	}
	// testCase 表示当前参数校验子场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if // err 是当前无效输入返回的应用错误。
			_, err := service.CollectBatch(context.Background(), testCase.input); !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
	if _, err := NewLinkImportService(nil, collector); err == nil {
		t.Fatal("missing resolver should fail")
	}
}
