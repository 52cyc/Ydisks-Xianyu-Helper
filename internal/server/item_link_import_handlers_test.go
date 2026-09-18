package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// itemLinkImportHandlerPort 为 HTTP handler 提供可控的批量采集结果和顶层错误。
type itemLinkImportHandlerPort struct {
	// result 是测试配置的逐条采集结果。
	result itemapp.LinkImportResult
	// err 是测试配置的顶层应用错误。
	err error
	// input 保存 handler 转换后的最近一次应用输入。
	input itemapp.LinkImportInput
}

// CollectBatch 记录应用输入并返回测试配置的结果或错误。
func (port *itemLinkImportHandlerPort) CollectBatch(_ context.Context, input itemapp.LinkImportInput) (itemapp.LinkImportResult, error) {
	port.input = input
	return port.result, port.err
}

// TestCollectItemLinksHandler 验证认证请求、应用输入转换、逐条结果响应和 OpenAPI 成功契约。
func TestCollectItemLinksHandler(t *testing.T) {
	// serverInstance 和 cleanup 是基础测试服务及资源释放函数。
	serverInstance, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 返回一条成功和一条失败，验证批量部分成功仍使用成功响应。
	port := &itemLinkImportHandlerPort{result: itemapp.LinkImportResult{
		Total: 2, Collected: 1, Failed: 1,
		Rows: []itemapp.LinkImportRow{
			{RowNo: 1, Source: "分享一", ItemID: "100", ItemURL: "https://www.goofish.com/item?id=100", Title: "商品", Description: "描述", Price: "9.90", Images: []string{"https://img.example/a.jpg"}},
			{RowNo: 2, Source: "无效", Error: "未找到可识别的闲鱼分享链接"},
		},
	}}
	serverInstance.applications.itemLinkImport = port
	// handler 是注入采集端口后的真实路由。
	handler := serverInstance.Router()
	// sessionCookie 是通过真实登录流程取得的管理员会话。
	sessionCookie := loginHelper(t, handler)
	// request 是带目标账号和两条分享文本的版本化请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/items/link-import/collect", strings.NewReader(`{"cookie_id":" account-1 ","sources":["分享一","无效"]}`))
	request.AddCookie(sessionCookie)
	// recorder 捕获 handler 的状态码和 JSON 响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"collected":1`) || !strings.Contains(recorder.Body.String(), `"error":"未找到可识别的闲鱼分享链接"`) {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if port.input.UserID <= 0 || port.input.CookieID != "account-1" || len(port.input.Sources) != 2 {
		t.Fatalf("application input = %+v", port.input)
	}
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
}

// TestCollectItemLinksHandlerValidationAndErrors 验证请求格式和应用顶层错误映射为统一非成功响应。
func TestCollectItemLinksHandlerValidationAndErrors(t *testing.T) {
	// serverInstance 和 cleanup 是基础测试服务及资源释放函数。
	serverInstance, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是可在子场景间切换错误的采集应用替身。
	port := &itemLinkImportHandlerPort{}
	serverInstance.applications.itemLinkImport = port
	// handler 和 sessionCookie 分别是真实路由和认证会话。
	handler := serverInstance.Router()
	sessionCookie := loginHelper(t, handler)
	// malformed 是无法解析的 JSON 请求。
	malformed := serveChatCoverageRequest(handler, sessionCookie, http.MethodPost, "/api/v1/items/link-import/collect", `{`)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("malformed status=%d body=%s", malformed.Code, malformed.Body.String())
	}
	// cases 保存应用顶层错误和目标 HTTP 状态码。
	cases := []struct {
		// name 是子测试名称。
		name string
		// err 是采集服务返回的应用错误。
		err error
		// status 是期望 HTTP 状态码。
		status int
	}{
		{name: "invalid account", err: itemapp.ErrLinkImportInvalidAccount, status: http.StatusBadRequest},
		{name: "empty sources", err: itemapp.ErrLinkImportNoSources, status: http.StatusBadRequest},
		{name: "too many", err: itemapp.ErrLinkImportTooManySources, status: http.StatusBadRequest},
		{name: "invalid user", err: itemapp.ErrLinkImportInvalidUser, status: http.StatusUnauthorized},
		{name: "internal", err: errors.New("dependency failed"), status: http.StatusInternalServerError},
	}
	// testCase 表示当前错误映射子场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			port.err = testCase.err
			// recorder 保存当前应用错误的 HTTP 映射结果。
			recorder := serveChatCoverageRequest(handler, sessionCookie, http.MethodPost, "/api/v1/items/link-import/collect", `{"cookie_id":"account-1","sources":["https://www.goofish.com/item?id=100000"]}`)
			if recorder.Code != testCase.status {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, testCase.status, recorder.Body.String())
			}
		})
	}
}
