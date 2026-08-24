package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedFulfillmentRoutes 挂载多实例卡速售 v2 履约 API。
func (s *Server) mountVersionedFulfillmentRoutes(router chi.Router) {
	// 回调路由不使用登录会话，由不可枚举实例 ID 和 API Key 签名共同保护。
	router.Post("/api/v1/integrations/kasushou-v2/{instance_public_id}/order-callback", s.fulfillmentOrderCallback)
	router.Group(func(router chi.Router) {
		router.Use(s.Auth.Middleware)
		router.Use(auth.RequireAuth)
		router.Get("/api/v1/fulfillment/instances", s.listFulfillmentInstances)
		router.Post("/api/v1/fulfillment/instances", s.createFulfillmentInstance)
		router.Put("/api/v1/fulfillment/instances/{instance_id}", s.updateFulfillmentInstance)
		router.Delete("/api/v1/fulfillment/instances/{instance_id}", s.deleteFulfillmentInstance)
		router.Get("/api/v1/fulfillment/instances/{instance_id}/products", s.listFulfillmentProducts)
		router.Get("/api/v1/fulfillment/instances/{instance_id}/products/{goods_id}", s.getFulfillmentProduct)
		router.Get("/api/v1/fulfillment/mappings", s.listFulfillmentMappings)
		router.Post("/api/v1/fulfillment/mappings", s.createFulfillmentMapping)
		router.Delete("/api/v1/fulfillment/mappings/{mapping_id}", s.deleteFulfillmentMapping)
		router.Get("/api/v1/fulfillment/orders", s.listFulfillmentOrders)
		router.Post("/api/v1/fulfillment/orders", s.purchaseFulfillmentOrder)
		router.Post("/api/v1/fulfillment/orders/{external_order_no}/refresh", s.refreshFulfillmentOrder)
	})
}
