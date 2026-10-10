package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestSnapshotTokenRecovery 验证分享链接采集沿用详情接口的 Token 恢复、耗尽和失败语义；t 管理本地 HTTP 生命周期。
func TestSnapshotTokenRecovery(t *testing.T) {
	// mode 选择响应下发新 Token、连续过期或主动刷新失败三种确定性场景。
	for _, mode := range []string{"updated", "exhausted", "refresh-failed"} {
		t.Run(mode, func(t *testing.T) {
			// requests 统计跨 HTTP 服务协程的请求次数。
			var requests atomic.Int32
			// server 只在本地模拟详情与 Token 接口；w 输出合成响应，r 用于区分端点。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// requestNumber 是当前详情或刷新请求的全局顺序。
				requestNumber := requests.Add(1)
				if strings.HasPrefix(r.URL.Path, "/token") {
					fmt.Fprint(w, `{"ret":["FAIL_BIZ_DENIED::本地模拟刷新失败"]}`)
					return
				}
				if mode == "updated" && requestNumber > 1 {
					fmt.Fprint(w, `{"ret":["SUCCESS::调用成功"],"data":{"title":"采集商品","price":"1.00","pics":["https://example.com/item.png"]}}`)
					return
				}
				if mode != "refresh-failed" {
					http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: fmt.Sprintf("synthetic%d_1", requestNumber), Path: "/"})
				}
				fmt.Fprint(w, `{"ret":["FAIL_SYS_TOKEN_EXOIRED::本地模拟签名过期"]}`)
			}))
			defer server.Close()
			// client 将详情与主动 Token 刷新都限制到同一本地服务。
			client := &ClientImpl{HTTPClient: server.Client(), ItemDetailURL: server.URL + "/detail", TokenURL: server.URL + "/token"}
			// ctx 携带已存在的平面 Cookie 会话，覆盖查询复用会话的路径。
			ctx, _ := WithFlatCookieSession(context.Background(), "unb=123; _m_h5_tk=initial_1")
			// snapshot、err 是恢复结束后的商品快照和错误。
			snapshot, err := client.FetchItemSnapshot(ctx, "unb=ignored", "item")
			if mode == "updated" {
				if err != nil || snapshot.Title != "采集商品" || requests.Load() != 2 {
					t.Fatalf("新 Token 重试失败: title=%q err=%v requests=%d", snapshot.Title, err, requests.Load())
				}
			} else if err == nil || snapshot.Title != "" {
				t.Fatal("恢复失败后不能返回商品快照")
			}
			if mode == "exhausted" && requests.Load() != 4 {
				t.Fatalf("应在四次详情请求后停止: %d", requests.Load())
			}
		})
	}
}
