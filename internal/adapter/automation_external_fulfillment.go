package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
	"xianyu-go/internal/automation"
)

// automationExternalFulfillmentAdapter 把通用货源应用服务转换为自动化执行器的窄端口。
type automationExternalFulfillmentAdapter struct {
	// service 提供幂等采购、原单查询和加密结果持久化。
	service *fulfillmentapp.Service
}

// newAutomationExternalFulfillmentAdapter 构造自动化专用的外部货源适配器。
func newAutomationExternalFulfillmentAdapter(service *fulfillmentapp.Service) automation.ExternalFulfillment {
	if service == nil {
		return nil
	}
	return &automationExternalFulfillmentAdapter{service: service}
}

// QuoteProduct 读取指定用户货源实例中的实时商品价；该调用只查询商品，不创建采购订单。
func (adapter *automationExternalFulfillmentAdapter) QuoteProduct(ctx context.Context, userID, instanceID, goodsID int64) (automation.ExternalProductQuote, error) {
	// product 是货源应用层归一化后的实时商品详情。
	product, productErr := adapter.service.GetProduct(ctx, userID, instanceID, goodsID)
	if productErr != nil {
		return automation.ExternalProductQuote{}, externalProductQuoteError(productErr)
	}
	// canBuy 兼容部分卡速售站点不返回 can_buy、但用 status=1 表示商品正常销售的响应。
	canBuy := product.CanBuy || product.Status == 1
	return automation.ExternalProductQuote{Price: strings.TrimSpace(product.Price), CanBuy: canBuy}, nil
}

// externalProductQuoteError 把货源应用层的商品删除事实转换为自动化消费者自己的稳定错误。
func externalProductQuoteError(err error) error {
	if errors.Is(err, fulfillmentapp.ErrProductNotFound) {
		return fmt.Errorf("%w: %v", automation.ErrExternalProductNotFound, err)
	}
	return err
}

// Fulfill 先幂等建单，对已有或处理中订单只使用原外部单号查询。
func (adapter *automationExternalFulfillmentAdapter) Fulfill(ctx context.Context, request automation.ExternalFulfillmentRequest) (result automation.ExternalFulfillmentResult, resultErr error) {
	defer func() {
		if errors.Is(resultErr, fulfillmentapp.ErrSafePriceExceeded) {
			resultErr = fmt.Errorf("%w: %v", automation.ErrExternalSafePriceExceeded, resultErr)
		}
	}()
	// purchaseRequest 是通用货源应用层的采购输入。
	purchaseRequest := fulfillmentapp.PurchaseRequest{InstanceID: request.InstanceID, ExternalOrderNo: request.ExternalOrderNo, XianyuOrderID: request.XianyuOrderID, RemoteGoodsID: request.GoodsID, Quantity: request.Quantity, SafePrice: request.SafePrice, Attach: request.Attach}
	// order、purchaseErr 分别是幂等采购返回的本地订单和远程结果错误。
	order, purchaseErr := adapter.service.Purchase(ctx, request.UserID, purchaseRequest)
	// 供应站已明确拒绝保护价时直接交给自动化重试和买家通知，不能用查单等待覆盖失败原因。
	if errors.Is(purchaseErr, fulfillmentapp.ErrSafePriceExceeded) {
		return automation.ExternalFulfillmentResult{}, purchaseErr
	}
	if purchaseErr == nil && fulfillmentOrderIsTerminal(order.State) {
		if fulfillmentapp.TerminalRetryRequired(order) {
			order, purchaseErr = adapter.service.ReplaceTerminalOrder(ctx, request.UserID, purchaseRequest)
		} else {
			order, purchaseErr = adapter.service.MarkTerminalRetryRequired(ctx, request.UserID, order.ExternalOrderNo)
		}
	}
	if purchaseErr != nil || fulfillmentOrderNeedsRefresh(order.State) {
		// refreshed、refreshErr 使用同一外部单号查询，绝不产生第二笔采购。
		refreshed, refreshErr := adapter.service.RefreshOrder(ctx, request.UserID, order.ExternalOrderNo)
		if refreshErr == nil {
			order = refreshed
			purchaseErr = nil
		} else if errors.Is(refreshErr, fulfillmentapp.ErrNotFound) {
			// retryRequest 复用当前实际履约单号和原采购参数。
			retryRequest := purchaseRequest
			retryRequest.ExternalOrderNo = order.ExternalOrderNo
			// retried、retryErr 分别是原外部单号明确查无结果后的安全重提订单和错误。
			retried, retryErr := adapter.service.RetryPurchaseAfterNotFound(ctx, request.UserID, retryRequest)
			if retryErr != nil {
				return automation.ExternalFulfillmentResult{}, fmt.Errorf("原单不存在，使用同一单号重新下单失败: %w", retryErr)
			}
			order = retried
			purchaseErr = nil
		} else if purchaseErr != nil {
			return automation.ExternalFulfillmentResult{}, fmt.Errorf("下单结果未确认: %v; 原单查询失败: %w", purchaseErr, refreshErr)
		} else {
			return automation.ExternalFulfillmentResult{}, refreshErr
		}
	}
	if purchaseErr == nil && fulfillmentOrderIsTerminal(order.State) && !fulfillmentapp.TerminalRetryRequired(order) {
		// marked 是首次观察到远程终态后写入的人工替代采购标记。
		marked, markErr := adapter.service.MarkTerminalRetryRequired(ctx, request.UserID, order.ExternalOrderNo)
		if markErr != nil {
			return automation.ExternalFulfillmentResult{}, markErr
		}
		order = marked
	}
	return automation.ExternalFulfillmentResult{State: order.State, Cards: append([]string(nil), order.CardList...), RechargeInfo: order.RechargeInfo, RechargeHints: order.RechargeHints}, nil
}

// fulfillmentOrderIsTerminal 判断订单是否已取消或退款，需要人工确认后创建替代单。
func fulfillmentOrderIsTerminal(state string) bool {
	return state == "cancelled" || state == "refunded"
}

// fulfillmentOrderNeedsRefresh 判断当前状态是否还需要向货源站查询最新结果。
func fulfillmentOrderNeedsRefresh(state string) bool {
	switch strings.TrimSpace(state) {
	case "succeeded", "failed", "cancelled", "refunded":
		return false
	default:
		return true
	}
}
