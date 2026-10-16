package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/auth"
)

// cardAPIEditorResponse 是用户主动编辑时的脱敏 DTO：只含非敏感字段与模板键名，不包含任何模板值且禁止缓存。
type cardAPIEditorResponse struct {
	// URL 是请求目标。
	URL string `json:"url"`
	// Method 是请求方法。
	Method string `json:"method"`
	// TimeoutSeconds 是超时秒数。
	TimeoutSeconds int `json:"timeout_seconds"`
	// ContentType 是正文编码。
	ContentType string `json:"content_type"`
	// HeaderKeys 是已保存请求头的顶层键名；值可能是凭证，永不下发。
	HeaderKeys []string `json:"header_keys"`
	// ParamKeys 是已保存查询参数的顶层键名，不含值。
	ParamKeys []string `json:"param_keys"`
	// BodyKeys 是已保存请求正文的顶层键名，不含值。
	BodyKeys []string `json:"body_keys"`
	// ResponsePath 是结果提取路径。
	ResponsePath string `json:"response_path"`
	// RetryEnabled 表示幂等重试开关。
	RetryEnabled bool `json:"retry_enabled"`
}

// getCardAPIEditor 读取当前会话拥有的卡密脱敏编辑视图，模板值只留在服务端。
func (s *Server) getCardAPIEditor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// cardID、err 是 URL 中的卡密标识及解析错误。
	cardID, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "卡券ID无效")
		return
	}
	// session 是已认证的当前调用者，不接受请求体指定所有者。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "请先登录")
		return
	}
	// editor、err 保存经所有权验证的脱敏编辑模型。
	editor, err := s.cardsApplication().GetAPIEditor(r.Context(), session.UserID, cardID)
	if err != nil {
		writeCardAPIError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cardAPIEditorResponse{URL: editor.URL, Method: editor.Method, TimeoutSeconds: editor.TimeoutSeconds,
		ContentType: editor.ContentType, HeaderKeys: editor.HeaderKeys, ParamKeys: editor.ParamKeys, BodyKeys: editor.BodyKeys,
		ResponsePath: editor.ResponsePath, RetryEnabled: editor.RetryEnabled})
}

// writeCardAPIError 将配置校验与资源归属错误映射为统一错误响应，基础设施错误不泄漏内部文本。
func writeCardAPIError(w http.ResponseWriter, err error) {
	// validation 是允许向用户展示的无秘密校验提示。
	var validation *cardsapp.ValidationError
	if errors.As(err, &validation) {
		writeErr(w, http.StatusBadRequest, validation.Message)
		return
	}
	writeCardReadError(w, err)
}
