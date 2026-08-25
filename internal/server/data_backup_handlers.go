package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	databackupapp "xianyu-go/internal/application/databackup"
	"xianyu-go/internal/auth"
)

const (
	// maxDataRestoreMultipartBytes 为 512 MiB 数据库文件额外保留 1 MiB multipart 元数据空间。
	maxDataRestoreMultipartBytes int64 = databackupapp.MaxRestoreBytes + (1 << 20)
)

// dataRestoreResponse 是恢复文件校验并暂存成功后的具名 HTTP 响应。
type dataRestoreResponse struct {
	// Filename 是已接收文件的安全基础名。
	Filename string `json:"filename"`
	// Size 是暂存文件字节数。
	Size int64 `json:"size"`
	// RestartRequired 提醒客户端当前进程仍使用原数据库。
	RestartRequired bool `json:"restart_required"`
	// Message 是不包含文件系统路径的后续操作提示。
	Message string `json:"message"`
}

// downloadDataBackup 为当前管理员流式返回 SQLite 一致性数据库快照。
func (server *Server) downloadDataBackup(responseWriter http.ResponseWriter, request *http.Request) {
	// session 是管理员中间件已经验证的当前会话。
	session := auth.SessionFromContext(request.Context())
	// artifact、exportErr 分别是一次性快照及创建失败原因。
	artifact, exportErr := server.dataBackupApplication().Export(request.Context(), session.UserID)
	if exportErr != nil {
		writeDataBackupError(responseWriter, exportErr)
		return
	}
	defer artifact.Reader.Close()
	responseWriter.Header().Set("Content-Type", "application/vnd.sqlite3")
	responseWriter.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(artifact.Filename))
	responseWriter.Header().Set("Content-Length", strconv.FormatInt(artifact.Size, 10))
	responseWriter.Header().Set("Cache-Control", "no-store")
	responseWriter.Header().Set("X-Content-Type-Options", "nosniff")
	responseWriter.WriteHeader(http.StatusOK)
	// copyErr 表示客户端中断或响应写入失败；响应已开始后不能再写 JSON 错误体。
	if _, copyErr := io.Copy(responseWriter, artifact.Reader); copyErr != nil {
		server.Logger.Warn("数据库备份下载中断", "err", copyErr)
	}
}

// importDataBackup 接收管理员确认的 SQLite 文件，校验后仅暂存到下次服务启动。
func (server *Server) importDataBackup(responseWriter http.ResponseWriter, request *http.Request) {
	if !parseMultipartRequest(responseWriter, request, maxDataRestoreMultipartBytes, 32<<20, "恢复上传内容不能超过 513 MiB") {
		return
	}
	if strings.TrimSpace(request.FormValue("confirmation")) != "RESTORE" {
		writeErr(responseWriter, http.StatusBadRequest, "请输入 RESTORE 确认恢复")
		return
	}
	// source、header、fileErr 分别是上传文件流、元数据和读取错误。
	source, header, fileErr := request.FormFile("file")
	if fileErr != nil {
		writeErr(responseWriter, http.StatusBadRequest, "请选择数据库备份文件")
		return
	}
	defer source.Close()
	// session 是管理员中间件已经验证的当前会话。
	session := auth.SessionFromContext(request.Context())
	// result、importErr 分别是校验暂存结果及失败原因。
	result, importErr := server.dataBackupApplication().Import(request.Context(), session.UserID, source, header.Filename)
	if importErr != nil {
		writeDataBackupError(responseWriter, importErr)
		return
	}
	writeJSON(responseWriter, http.StatusOK, dataRestoreResponse{
		Filename: result.Filename, Size: result.Size, RestartRequired: result.RestartRequired,
		Message: "备份已校验并暂存，请重启服务完成恢复",
	})
}

// writeDataBackupError 将备份应用与数据库错误映射为不泄露本机路径的统一 HTTP 错误。
func writeDataBackupError(responseWriter http.ResponseWriter, operationErr error) {
	switch {
	case errors.Is(operationErr, databackupapp.ErrUnsupported):
		writeErr(responseWriter, http.StatusUnprocessableEntity, operationErr.Error())
	case errors.Is(operationErr, databackupapp.ErrInvalidUser):
		writeErr(responseWriter, http.StatusUnauthorized, "登录状态无效")
	default:
		writeErr(responseWriter, http.StatusBadRequest, "备份文件校验或处理失败，请确认文件有效后重试")
	}
}
