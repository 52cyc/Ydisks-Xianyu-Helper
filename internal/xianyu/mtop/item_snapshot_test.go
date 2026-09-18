package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchItemSnapshot 验证官方详情字段被转换为标题、描述、元金额、去重图片和多规格标记。
func TestFetchItemSnapshot(t *testing.T) {
	// server 返回包含重复图片和多 SKU 的确定性平台详情响应。
	server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("api") != "mtop.taobao.idle.pc.detail" {
			t.Fatalf("unexpected api query: %s", request.URL.RawQuery)
		}
		_, _ = fmt.Fprint(responseWriter, `{"ret":["SUCCESS::调用成功"],"data":{"itemTextDTO":{"title":"测试商品","desc":"测试描述"},"itemPriceDTO":{"priceInCent":"1299"},"imageInfoDOList":[{"url":"https://img.example/a.jpg"},{"url":"https://img.example/a.jpg"},{"imageUrl":"https://img.example/b.jpg"}],"skuList":[{"id":"1"},{"id":"2"}],"recommend":{"title":"不能采集"}}}`)
	}))
	defer server.Close()
	// client 是详情端点指向本地服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), ItemDetailURL: server.URL}
	// snapshot 和 snapshotErr 保存采集后的非敏感商品快照。
	snapshot, snapshotErr := client.FetchItemSnapshot(context.Background(), "_m_h5_tk=token_1", "100")
	if snapshotErr != nil {
		t.Fatalf("FetchItemSnapshot() error = %v", snapshotErr)
	}
	if snapshot.Title != "测试商品" || snapshot.Description != "测试描述" || snapshot.PriceText != "12.99" || !snapshot.IsMultiSpec {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if len(snapshot.ImageURLs) != 2 || snapshot.ImageURLs[1] != "https://img.example/b.jpg" {
		t.Fatalf("images = %v", snapshot.ImageURLs)
	}
}

// TestItemSnapshotCompatibility 验证嵌套详情、元价格、主图回退和推荐商品隔离。
func TestItemSnapshotCompatibility(t *testing.T) {
	// snapshot 是从兼容 itemDO 节点提取的单规格详情。
	snapshot := itemSnapshotFromDetail(map[string]any{
		"itemDO":    map[string]any{"itemTitle": "兼容标题", "description": "兼容描述", "price": map[string]any{"value": "¥1,234.5"}, "mainPic": "https://img.example/main.png"},
		"recommend": map[string]any{"title": "推荐标题", "price": "0.01", "picUrl": "https://img.example/recommend.png", "skuList": []any{map[string]any{"id": "1"}, map[string]any{"id": "2"}}},
	})
	if snapshot.Title != "兼容标题" || snapshot.PriceText != "1234.50" || len(snapshot.ImageURLs) != 1 || snapshot.IsMultiSpec {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

// TestFetchItemSnapshotRejectsIncompleteDetails 验证缺少必需字段时返回可展示但不含原始响应的错误。
func TestFetchItemSnapshotRejectsIncompleteDetails(t *testing.T) {
	// cases 保存依次缺少标题、价格和图片的响应及目标错误片段。
	cases := []struct {
		// name 是子测试名称。
		name string
		// data 是平台详情 data 对象。
		data string
		// want 是安全错误应包含的字段说明。
		want string
	}{
		{name: "title", data: `{"price":"1","picUrl":"https://img.example/a.jpg"}`, want: "标题"},
		{name: "price", data: `{"title":"商品","picUrl":"https://img.example/a.jpg"}`, want: "价格"},
		{name: "image", data: `{"title":"商品","price":"1"}`, want: "图片"},
	}
	// testCase 表示当前不完整详情子场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// server 返回当前子场景的不完整平台详情。
			server := httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(responseWriter, `{"ret":["SUCCESS::调用成功"],"data":%s}`, testCase.data)
			}))
			defer server.Close()
			// client 是当前本地详情端点的 MTOP 客户端。
			client := &ClientImpl{HTTPClient: server.Client(), ItemDetailURL: server.URL}
			if // snapshotErr 是当前缺字段详情返回的错误。
			_, snapshotErr := client.FetchItemSnapshot(context.Background(), "_m_h5_tk=token_1", "100"); snapshotErr == nil || !strings.Contains(snapshotErr.Error(), testCase.want) {
				t.Fatalf("error = %v, want contains %q", snapshotErr, testCase.want)
			}
		})
	}
}
