package server

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	databackupapp "xianyu-go/internal/application/databackup"
)

// controllableDataBackupPort 为 HTTP 契约测试返回固定快照并记录导入文件信息。
type controllableDataBackupPort struct {
	// importedName 是 Import 收到的原始文件名。
	importedName string
	// importedBytes 是 Import 读取的上传内容，不包含 multipart 元数据。
	importedBytes []byte
}

// Export 返回可由下载 handler 完整关闭的固定 SQLite 头部夹具。
func (port *controllableDataBackupPort) Export(context.Context, int64) (databackupapp.Artifact, error) {
	// content 是测试下载响应使用的最小固定字节，不作为真实 SQLite 文件导入。
	content := []byte("SQLite format 3\x00fixture")
	return databackupapp.Artifact{Reader: io.NopCloser(bytes.NewReader(content)), Filename: "ydisks-backup.db", Size: int64(len(content))}, nil
}

// Import 读取上传文件并返回已暂存响应，验证 transport 保留文件字节与安全名称。
func (port *controllableDataBackupPort) Import(_ context.Context, _ int64, source io.Reader, filename string) (databackupapp.RestoreResult, error) {
	// content、readErr 分别是 handler 传入的文件字节和读取失败原因。
	content, readErr := io.ReadAll(source)
	if readErr != nil {
		return databackupapp.RestoreResult{}, readErr
	}
	port.importedName, port.importedBytes = filename, content
	return databackupapp.RestoreResult{Filename: filename, Size: int64(len(content)), RestartRequired: true}, nil
}

// TestDataBackupRoutes 验证管理员二进制下载头、恢复 multipart 及 OpenAPI 成功响应契约。
func TestDataBackupRoutes(t *testing.T) {
	// serverInstance、cleanup 分别是测试 HTTP 服务和资源清理函数。
	serverInstance, _, cleanup := newTestServer(t)
	defer cleanup()
	// backupPort 是本场景注入的可控备份应用端口。
	backupPort := &controllableDataBackupPort{}
	serverInstance.applications.dataBackup = backupPort
	// handler 是包含管理员鉴权和版本化路由的真实 Router。
	handler := serverInstance.Router()
	// sessionCookie 是管理员登录后的认证会话。
	sessionCookie := loginHelper(t, handler)

	// downloadRequest、downloadRecorder 分别是备份下载请求和响应记录器。
	downloadRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/data-backup", nil)
	downloadRequest.AddCookie(sessionCookie)
	// downloadRecorder 捕获数据库快照字节和安全响应头。
	downloadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(downloadRecorder, downloadRequest)
	if downloadRecorder.Code != http.StatusOK || downloadRecorder.Header().Get("Cache-Control") != "no-store" || downloadRecorder.Header().Get("Content-Disposition") == "" {
		t.Fatalf("下载响应 status=%d headers=%v", downloadRecorder.Code, downloadRecorder.Header())
	}
	if downloadRecorder.Body.String() != "SQLite format 3\x00fixture" {
		t.Fatalf("下载内容=%q", downloadRecorder.Body.String())
	}

	// multipartBody、multipartWriter 负责构造真实恢复上传表单。
	var multipartBody bytes.Buffer
	// multipartWriter 负责写入文件字段、确认字段和合法 boundary。
	multipartWriter := multipart.NewWriter(&multipartBody)
	// fileWriter、createErr 分别是数据库文件字段 writer 和创建错误。
	fileWriter, createErr := multipartWriter.CreateFormFile("file", "restore.db")
	if createErr != nil {
		t.Fatalf("创建恢复文件字段失败: %v", createErr)
	}
	if _, writeErr := fileWriter.Write([]byte("database-fixture")); writeErr != nil { // writeErr 表示测试数据库字节写入表单失败。
		t.Fatalf("写入恢复文件失败: %v", writeErr)
	}
	if fieldErr := multipartWriter.WriteField("confirmation", "RESTORE"); fieldErr != nil { // fieldErr 表示恢复确认词写入失败。
		t.Fatalf("写入确认字段失败: %v", fieldErr)
	}
	if closeErr := multipartWriter.Close(); closeErr != nil { // closeErr 表示 multipart 结束 boundary 写入失败。
		t.Fatalf("关闭 multipart 失败: %v", closeErr)
	}
	// restoreRequest、restoreRecorder 分别是恢复暂存请求和响应记录器。
	restoreRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/data-restore", &multipartBody)
	restoreRequest.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	restoreRequest.AddCookie(sessionCookie)
	// restoreRecorder 捕获恢复暂存的 JSON 成功响应。
	restoreRecorder := httptest.NewRecorder()
	handler.ServeHTTP(restoreRecorder, restoreRequest)
	assertOpenAPISuccessResponse(t, restoreRequest, restoreRecorder)
	if backupPort.importedName != "restore.db" || string(backupPort.importedBytes) != "database-fixture" {
		t.Fatalf("导入 name=%q content=%q", backupPort.importedName, string(backupPort.importedBytes))
	}
}
