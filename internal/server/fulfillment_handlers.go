package server

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
	"xianyu-go/internal/auth"
)

// fulfillmentInstanceListResponse 是货源实例列表响应。
type fulfillmentInstanceListResponse struct {
	Data []fulfillmentapp.Instance `json:"data"`
}

// fulfillmentProductListResponse 是远程商品列表响应。
type fulfillmentProductListResponse struct {
	Data []fulfillmentapp.Product `json:"data"`
	// Total 是当前筛选条件下的远程商品总数。
	Total int `json:"total"`
	// Page 是当前页码。
	Page int `json:"page"`
	// PageSize 是当前请求的单页数量。
	PageSize int `json:"page_size"`
	// TotalPages 是供应站声明或按总数推导的总页数。
	TotalPages int `json:"total_pages,omitempty"`
}

// fulfillmentCategoryListResponse 是货源商品目录树响应。
type fulfillmentCategoryListResponse struct {
	// Data 保存供应站返回的所有顶级目录。
	Data []fulfillmentapp.Category `json:"data"`
}

// fulfillmentMappingListResponse 是商品映射列表响应。
type fulfillmentMappingListResponse struct {
	Data []fulfillmentapp.Mapping `json:"data"`
}

// fulfillmentOrderListResponse 是履约订单列表响应。
type fulfillmentOrderListResponse struct {
	Data []fulfillmentapp.Order `json:"data"`
}

// listFulfillmentInstances 列出当前用户配置的多个外部货源实例。
func (s *Server) listFulfillmentInstances(w http.ResponseWriter, r *http.Request) {
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// instances 是不包含 API 密钥明文的实例列表。
	instances, err := s.fulfillmentApplication().ListInstances(r.Context(), session.UserID)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fulfillmentInstanceListResponse{Data: instances})
}

// createFulfillmentInstance 创建受支持协议的外部货源实例。
func (s *Server) createFulfillmentInstance(w http.ResponseWriter, r *http.Request) {
	// input 是请求体中的实例配置。
	var input fulfillmentapp.InstanceInput
	if // decodeErr 是实例创建请求解码错误。
	decodeErr := decodeJSON(r, &input); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "货源实例请求无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// instance 是已加密密钥后创建的实例。
	instance, err := s.fulfillmentApplication().CreateInstance(r.Context(), session.UserID, input)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, instance)
}

// updateFulfillmentInstance 更新实例，空 API Key 表示保留原密钥。
func (s *Server) updateFulfillmentInstance(w http.ResponseWriter, r *http.Request) {
	// instanceID 是路径中的货源实例数据库主键。
	instanceID, err := strconv.ParseInt(chi.URLParam(r, "instance_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "货源实例 ID 无效")
		return
	}
	// input 是请求体中的更新配置。
	var input fulfillmentapp.InstanceInput
	if // decodeErr 是实例更新请求解码错误。
	decodeErr := decodeJSON(r, &input); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "货源实例请求无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// instance 是更新后的非敏感实例。
	instance, err := s.fulfillmentApplication().UpdateInstance(r.Context(), session.UserID, instanceID, input)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, instance)
}

// deleteFulfillmentInstance 删除当前用户未被映射或订单引用的实例。
func (s *Server) deleteFulfillmentInstance(w http.ResponseWriter, r *http.Request) {
	// instanceID 是路径中的货源实例主键。
	instanceID, err := strconv.ParseInt(chi.URLParam(r, "instance_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "货源实例 ID 无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	if // deleteErr 是货源实例删除错误。
	deleteErr := s.fulfillmentApplication().DeleteInstance(r.Context(), session.UserID, instanceID); deleteErr != nil {
		writeFulfillmentError(w, deleteErr)
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// listFulfillmentProducts 从指定货源站读取商品。
func (s *Server) listFulfillmentProducts(w http.ResponseWriter, r *http.Request) {
	// instanceID 是当前要请求的货源实例。
	instanceID, err := strconv.ParseInt(chi.URLParam(r, "instance_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "货源实例 ID 无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// query 是目录、关键词和页码组成的商品查询条件。
	query := fulfillmentapp.ProductListQuery{
		CategoryID: int64(atoiDefault(r.URL.Query().Get("category_id"), 0)),
		Keyword:    r.URL.Query().Get("keyword"), Page: atoiDefault(r.URL.Query().Get("page"), 1),
		PageSize: atoiDefault(r.URL.Query().Get("page_size"), 50),
	}
	// productPage 是远程站的标准化商品分页结果。
	productPage, err := s.fulfillmentApplication().ListProductPage(r.Context(), session.UserID, instanceID, query)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fulfillmentProductListResponse{Data: productPage.Items, Total: productPage.Total, Page: productPage.Page, PageSize: productPage.PageSize, TotalPages: productPage.TotalPages})
}

// listFulfillmentCategories 从指定目录型货源实例读取商品目录树。
func (s *Server) listFulfillmentCategories(w http.ResponseWriter, r *http.Request) {
	// instanceID 是当前要请求的货源实例。
	instanceID, parseErr := strconv.ParseInt(chi.URLParam(r, "instance_id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "货源实例 ID 无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// categories 是远程站的标准化目录树。
	categories, listErr := s.fulfillmentApplication().ListCategories(r.Context(), session.UserID, instanceID)
	if listErr != nil {
		writeFulfillmentError(w, listErr)
		return
	}
	writeJSON(w, http.StatusOK, fulfillmentCategoryListResponse{Data: categories})
}

// getFulfillmentProduct 读取商品详情和直充动态字段。
func (s *Server) getFulfillmentProduct(w http.ResponseWriter, r *http.Request) {
	// instanceID 是货源实例主键。
	instanceID, instanceErr := strconv.ParseInt(chi.URLParam(r, "instance_id"), 10, 64)
	// goodsID 是货源站商品主键。
	goodsID, goodsErr := strconv.ParseInt(chi.URLParam(r, "goods_id"), 10, 64)
	if instanceErr != nil || goodsErr != nil {
		writeErr(w, http.StatusBadRequest, "货源实例或商品 ID 无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// product 是带动态 attach 字段的商品详情。
	product, err := s.fulfillmentApplication().GetProduct(r.Context(), session.UserID, instanceID, goodsID)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, product)
}

// listFulfillmentMappings 列出当前用户的闲鱼规格货源映射。
func (s *Server) listFulfillmentMappings(w http.ResponseWriter, r *http.Request) {
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// mappings 是当前用户的映射列表。
	mappings, err := s.fulfillmentApplication().ListMappings(r.Context(), session.UserID)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fulfillmentMappingListResponse{Data: mappings})
}

// createFulfillmentMapping 创建一条闲鱼商品规格到货源商品的映射。
func (s *Server) createFulfillmentMapping(w http.ResponseWriter, r *http.Request) {
	// input 是请求体中的映射配置。
	var input fulfillmentapp.MappingInput
	if // decodeErr 是商品映射创建请求解码错误。
	decodeErr := decodeJSON(r, &input); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "货源映射请求无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// mapping 是创建后的商品映射。
	mapping, err := s.fulfillmentApplication().CreateMapping(r.Context(), session.UserID, input)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapping)
}

// deleteFulfillmentMapping 删除当前用户的一条映射。
func (s *Server) deleteFulfillmentMapping(w http.ResponseWriter, r *http.Request) {
	// mappingID 是路径中的映射主键。
	mappingID, err := strconv.ParseInt(chi.URLParam(r, "mapping_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "货源映射 ID 无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	if // deleteErr 是商品映射删除错误。
	deleteErr := s.fulfillmentApplication().DeleteMapping(r.Context(), session.UserID, mappingID); deleteErr != nil {
		writeFulfillmentError(w, deleteErr)
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// purchaseFulfillmentOrder 创建一笔幂等采购，相同外部订单号不会重复下单。
func (s *Server) purchaseFulfillmentOrder(w http.ResponseWriter, r *http.Request) {
	// request 是请求体中的采购参数。
	var request fulfillmentapp.PurchaseRequest
	if // decodeErr 是幂等采购请求解码错误。
	decodeErr := decodeJSON(r, &request); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "货源下单请求无效")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// order 是创建或原幂等键对应的履约订单。
	order, err := s.fulfillmentApplication().Purchase(r.Context(), session.UserID, request)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

// listFulfillmentOrders 列出当前用户最新的履约订单。
func (s *Server) listFulfillmentOrders(w http.ResponseWriter, r *http.Request) {
	// limit 是请求的最大订单数量，应用服务会再限制范围。
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// orders 是已解密结果的归属内订单列表。
	orders, err := s.fulfillmentApplication().ListOrders(r.Context(), session.UserID, limit)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, fulfillmentOrderListResponse{Data: orders})
}

// refreshFulfillmentOrder 使用原外部订单号查询并更新远程状态。
func (s *Server) refreshFulfillmentOrder(w http.ResponseWriter, r *http.Request) {
	// externalOrderNo 是幂等下单和安全查询共用的外部单号。
	externalOrderNo := strings.TrimSpace(chi.URLParam(r, "external_order_no"))
	if externalOrderNo == "" {
		writeErr(w, http.StatusBadRequest, "外部订单号不能为空")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// order 是远程查询后更新的履约订单。
	order, err := s.fulfillmentApplication().RefreshOrder(r.Context(), session.UserID, externalOrderNo)
	if err != nil {
		writeFulfillmentError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

// fulfillmentOrderCallback 处理无会话的卡速售订单回调，只在验签和落库成功后返回 ok。
func (s *Server) fulfillmentOrderCallback(w http.ResponseWriter, r *http.Request) {
	// body 是受限长度的原始回调 JSON，验签必须使用原字段值。
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONRequestBytes+1))
	if err != nil || len(body) > maxJSONRequestBytes {
		writeErr(w, http.StatusBadRequest, "回调请求无效")
		return
	}
	if // callbackErr 是回调验签或归属内订单更新错误。
	callbackErr := s.fulfillmentApplication().ApplyOrderCallback(r.Context(), chi.URLParam(r, "instance_public_id"), body); callbackErr != nil {
		writeErr(w, http.StatusBadRequest, "回调验签或订单更新失败")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// writeFulfillmentError 把外部履约应用错误转为统一 HTTP 错误。
func writeFulfillmentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fulfillmentapp.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, fulfillmentapp.ErrConflict):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}
