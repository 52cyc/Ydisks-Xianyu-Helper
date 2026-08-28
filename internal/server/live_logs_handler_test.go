package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/logging"
)

// TestLiveLogsEndpoint 验证实时日志仅对管理员开放，并遵循 OpenAPI 增量响应契约。
func TestLiveLogsEndpoint(t *testing.T) {
	// server、cleanup 分别是持有独立日志缓冲区的测试服务与资源释放函数。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// buffer 是测试组合根注入的真实有界日志源。
	buffer, ok := server.liveLogs.(*logging.LiveBuffer)
	if !ok {
		t.Fatal("测试服务未注入实时日志缓冲区")
	}
	_, _ = buffer.Write([]byte("time=2026-08-27T10:00:00+08:00 level=INFO msg=ready\n"))
	// handler 是包含真实会话与管理员中间件的完整路由。
	handler := server.Router()
	// unauthorizedRequest 验证未登录请求不能读取运行日志。
	unauthorizedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs", nil)
	// unauthorizedRecorder 捕获未登录的统一错误响应。
	unauthorizedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedRecorder, unauthorizedRequest)
	if unauthorizedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorizedRecorder.Code, unauthorizedRecorder.Body.String())
	}
	// sessionCookie 是通过真实登录端点获得的管理员会话。
	sessionCookie := loginHelper(t, handler)
	// request 携带游标和上限，验证真实版本化日志接口。
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs?after=0&limit=10", nil)
	request.AddCookie(sessionCookie)
	// recorder 捕获待进行 OpenAPI 校验的日志响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPISuccessResponse(t, request, recorder)
	// response 是解码后的具名日志响应 DTO。
	var response liveLogsResponse
	// decodeErr 是真实日志响应无法解码为具名 DTO 时的契约失败。
	decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response)
	if decodeErr != nil {
		t.Fatalf("decode live logs: %v", decodeErr)
	}
	if len(response.Lines) == 0 || response.Lines[0].Text == "" || response.NextCursor == 0 {
		t.Fatalf("response=%+v", response)
	}
	// invalidRequest 验证非数字游标不会被默认成全量读取。
	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/logs?after=invalid", nil)
	invalidRequest.AddCookie(sessionCookie)
	// invalidRecorder 捕获参数校验的统一错误包装。
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalidRequest)
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d body=%s", invalidRecorder.Code, invalidRecorder.Body.String())
	}
}
