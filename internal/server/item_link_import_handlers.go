package server

import (
	"errors"
	"net/http"
	"strings"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
)

// itemLinkImportRequest 是批量分享链接采集的具名 HTTP 请求体。
type itemLinkImportRequest struct {
	// CookieID 是用户选择的自有闲鱼账号标识。
	CookieID string `json:"cookie_id"`
	// Sources 是逐条分享文本或标准商品地址，一次最多五十条。
	Sources []string `json:"sources"`
}

// itemLinkImportRowResponse 是单条分享文本的采集结果，不包含账号凭证或卖家身份。
type itemLinkImportRowResponse struct {
	// RowNo 是去除空行后的从一开始序号。
	RowNo int `json:"row_no"`
	// Source 是用户已经提交的原始分享文本。
	Source string `json:"source"`
	// ItemID 是解析成功后的闲鱼商品标识。
	ItemID string `json:"item_id"`
	// ItemURL 是移除分享跟踪参数后的标准闲鱼商品地址。
	ItemURL string `json:"item_url"`
	// Title 是采集成功后的商品标题。
	Title string `json:"title"`
	// Description 是采集成功后的商品描述。
	Description string `json:"description"`
	// Price 是采集成功后的十进制元金额文本。
	Price string `json:"price"`
	// Images 是采集成功后的最多九张图片地址。
	Images []string `json:"images"`
	// Error 是当前条目的安全失败说明，成功时为空字符串。
	Error string `json:"error"`
}

// itemLinkImportResponse 是批量采集的具名成功响应。
type itemLinkImportResponse struct {
	// Success 表示服务已完成整批解析；逐条失败由 Rows 和 Failed 表达。
	Success bool `json:"success"`
	// Total 是参与处理的非空分享文本数量。
	Total int `json:"total"`
	// Collected 是可以进入现有批量预检的商品数量。
	Collected int `json:"collected"`
	// Failed 是解析、详情读取或能力校验失败的数量。
	Failed int `json:"failed"`
	// Rows 按用户输入顺序保存逐条结果。
	Rows []itemLinkImportRowResponse `json:"rows"`
}

// collectItemLinks 解析批量分享文本并使用所选自有账号采集公开商品详情，不在本请求中执行上架。
func (s *Server) collectItemLinks(responseWriter http.ResponseWriter, request *http.Request) {
	// session 保存认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(request.Context())
	if session == nil || session.UserID <= 0 {
		writeErr(responseWriter, http.StatusUnauthorized, "未登录")
		return
	}
	// input 保存 JSON 请求解码后的目标账号和分享文本集合。
	var input itemLinkImportRequest
	if // decodeErr 是请求体格式、大小或尾随内容错误。
	decodeErr := decodeJSON(request, &input); decodeErr != nil {
		writeErr(responseWriter, http.StatusBadRequest, "商品链接采集请求格式错误")
		return
	}
	// result 和 collectErr 保存应用层逐条采集结果及顶层参数错误。
	result, collectErr := s.itemLinkImportApplication().CollectBatch(request.Context(), itemapp.LinkImportInput{
		UserID: session.UserID, CookieID: strings.TrimSpace(input.CookieID), Sources: input.Sources,
	})
	if collectErr != nil {
		switch {
		case errors.Is(collectErr, itemapp.ErrLinkImportInvalidUser):
			writeErr(responseWriter, http.StatusUnauthorized, collectErr.Error())
		case errors.Is(collectErr, itemapp.ErrLinkImportInvalidAccount), errors.Is(collectErr, itemapp.ErrLinkImportNoSources), errors.Is(collectErr, itemapp.ErrLinkImportTooManySources):
			writeErr(responseWriter, http.StatusBadRequest, collectErr.Error())
		default:
			writeErr(responseWriter, http.StatusInternalServerError, "商品链接采集失败")
		}
		return
	}
	// rows 把应用模型转换为稳定 HTTP DTO，避免 transport 直接序列化应用结构。
	rows := make([]itemLinkImportRowResponse, 0, len(result.Rows))
	// row 表示当前待转换的应用层逐条采集结果。
	for _, row := range result.Rows {
		rows = append(rows, itemLinkImportRowResponse{
			RowNo: row.RowNo, Source: row.Source, ItemID: row.ItemID, ItemURL: row.ItemURL,
			Title: row.Title, Description: row.Description, Price: row.Price,
			Images: append([]string{}, row.Images...), Error: row.Error,
		})
	}
	writeJSON(responseWriter, http.StatusOK, itemLinkImportResponse{Success: true, Total: result.Total, Collected: result.Collected, Failed: result.Failed, Rows: rows})
}
