package fulfillment

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// terminalRetryMarker 标记终态订单已经等待用户确认创建替代采购单。
const terminalRetryMarker = "远程订单已终止，等待人工重新采购"

// Service 编排多实例货源配置、商品映射和幂等采购。
type Service struct {
	// repository 保存实例秘钥、映射和采购状态。
	repository Repository
	// gateway 将通用履约操作转换为卡速售 v2 协议请求。
	gateway Gateway
}

// NewService 创建外部货源履约应用服务。
func NewService(repository Repository, gateway Gateway) *Service {
	return &Service{repository: repository, gateway: gateway}
}

// ListInstances 返回不包含 API 密钥明文的货源实例。
func (service *Service) ListInstances(ctx context.Context, userID int64) ([]Instance, error) {
	return service.repository.ListInstances(ctx, userID)
}

// CreateInstance 校验并创建货源实例。
func (service *Service) CreateInstance(ctx context.Context, userID int64, input InstanceInput) (Instance, error) {
	input = NormalizeInstanceInput(input)
	if // validationErr 是创建实例前的输入校验错误。
	validationErr := validateInstanceInput(input, false); validationErr != nil {
		return Instance{}, validationErr
	}
	return service.repository.CreateInstance(ctx, userID, input)
}

// UpdateInstance 更新货源实例；空 APIKey 表示保留旧密钥。
func (service *Service) UpdateInstance(ctx context.Context, userID, instanceID int64, input InstanceInput) (Instance, error) {
	input = NormalizeInstanceInput(input)
	if // validationErr 是更新实例前的输入校验错误。
	validationErr := validateInstanceInput(input, true); validationErr != nil {
		return Instance{}, validationErr
	}
	return service.repository.UpdateInstance(ctx, userID, instanceID, input)
}

// DeleteInstance 删除当前用户名下且没有活动映射的货源实例。
func (service *Service) DeleteInstance(ctx context.Context, userID, instanceID int64) error {
	return service.repository.DeleteInstance(ctx, userID, instanceID)
}

// ListProducts 从指定货源站读取商品列表。
func (service *Service) ListProducts(ctx context.Context, userID, instanceID int64) ([]Product, error) {
	// instance 包含本次外部请求所需的短时秘钥视图。
	instance, err := service.repository.GetInstance(ctx, userID, instanceID, true)
	if err != nil {
		return nil, err
	}
	if !instance.Enabled {
		return nil, errors.New("货源实例已停用")
	}
	return service.gateway.ListProducts(ctx, instance)
}

// GetProduct 读取远程商品详情及直充附加字段。
func (service *Service) GetProduct(ctx context.Context, userID, instanceID, goodsID int64) (Product, error) {
	// instance 包含本次外部请求所需的短时秘钥视图。
	instance, err := service.repository.GetInstance(ctx, userID, instanceID, true)
	if err != nil {
		return Product{}, err
	}
	return service.gateway.GetProduct(ctx, instance, goodsID)
}

// ListMappings 返回当前用户的闲鱼商品与货源商品映射。
func (service *Service) ListMappings(ctx context.Context, userID int64) ([]Mapping, error) {
	return service.repository.ListMappings(ctx, userID)
}

// CreateMapping 校验并创建一条商品规格映射。
func (service *Service) CreateMapping(ctx context.Context, userID int64, input MappingInput) (Mapping, error) {
	if input.InstanceID <= 0 || input.RemoteGoodsID <= 0 || strings.TrimSpace(input.AccountID) == "" || strings.TrimSpace(input.ItemID) == "" {
		return Mapping{}, errors.New("映射缺少货源实例、远程商品、闲鱼账号或商品")
	}
	if input.QuantityMode == "" {
		input.QuantityMode = "order_quantity"
	}
	if input.QuantityMode != "order_quantity" && input.QuantityMode != "fixed" {
		return Mapping{}, errors.New("数量策略只支持 order_quantity 或 fixed")
	}
	if input.QuantityMode == "fixed" && input.FixedQuantity <= 0 {
		return Mapping{}, errors.New("固定数量必须大于 0")
	}
	return service.repository.CreateMapping(ctx, userID, input)
}

// DeleteMapping 删除当前用户的一条商品映射。
func (service *Service) DeleteMapping(ctx context.Context, userID, mappingID int64) error {
	return service.repository.DeleteMapping(ctx, userID, mappingID)
}

// Purchase 先持久化幂等采购单，再请求远程站并保存返回状态。
func (service *Service) Purchase(ctx context.Context, userID int64, request PurchaseRequest) (Order, error) {
	if request.InstanceID <= 0 || request.RemoteGoodsID <= 0 || request.Quantity <= 0 || strings.TrimSpace(request.ExternalOrderNo) == "" {
		return Order{}, errors.New("采购请求缺少实例、商品、数量或外部订单号")
	}
	// localOrder 是远程请求前创建的幂等采购记录。
	localOrder, created, err := service.repository.CreateOrder(ctx, userID, request)
	if err != nil {
		return Order{}, err
	}
	if !created {
		return localOrder, nil
	}
	// instance 是发起采购请求所需的货源实例秘钥视图。
	instance, err := service.repository.GetInstance(ctx, userID, request.InstanceID, true)
	if err != nil {
		return localOrder, err
	}
	// remoteOrder 是远程站接受采购后的当前状态。
	remoteOrder, err := service.gateway.Buy(ctx, instance, request)
	if err != nil {
		_ = service.repository.RecordOrderError(ctx, userID, request.ExternalOrderNo, err.Error())
		return localOrder, fmt.Errorf("远程下单结果未确认，请使用原外部订单号查询: %w", err)
	}
	return service.repository.ApplyRemoteOrder(ctx, userID, request.InstanceID, request.ExternalOrderNo, remoteOrder)
}

// RefreshOrder 使用原外部订单号查询远程状态，绝不产生第二笔采购。
func (service *Service) RefreshOrder(ctx context.Context, userID int64, externalOrderNo string) (Order, error) {
	// localOrder 提供实例归属和远程订单号。
	localOrder, err := service.repository.GetOrder(ctx, userID, strings.TrimSpace(externalOrderNo))
	if err != nil {
		return Order{}, err
	}
	// instance 是查询远程订单的秘钥视图。
	instance, err := service.repository.GetInstance(ctx, userID, localOrder.InstanceID, true)
	if err != nil {
		return Order{}, err
	}
	// remoteOrder 是查询接口返回的最新状态。
	remoteOrder, err := service.gateway.QueryOrder(ctx, instance, OrderQuery{ExternalOrderNo: localOrder.ExternalOrderNo, RemoteOrderNo: localOrder.RemoteOrderNo, CreatedAt: localOrder.CreatedAt})
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			_ = service.repository.RecordOrderError(ctx, userID, localOrder.ExternalOrderNo, err.Error())
		}
		return localOrder, err
	}
	return service.repository.ApplyRemoteOrder(ctx, userID, localOrder.InstanceID, localOrder.ExternalOrderNo, remoteOrder)
}

// RetryPurchaseAfterNotFound 在原外部单号明确查无订单后，使用完全相同的幂等键重新提交采购。
func (service *Service) RetryPurchaseAfterNotFound(ctx context.Context, userID int64, request PurchaseRequest) (Order, error) {
	// localOrder 是第一次提交前已经创建的本地幂等记录。
	localOrder, err := service.repository.GetOrder(ctx, userID, strings.TrimSpace(request.ExternalOrderNo))
	if err != nil {
		return Order{}, err
	}
	if localOrder.InstanceID != request.InstanceID || localOrder.RemoteGoodsID != request.RemoteGoodsID || localOrder.Quantity != request.Quantity {
		return localOrder, errors.New("原外部订单号对应的采购参数不一致")
	}
	// instance 是重新提交时使用的同一货源实例密钥视图。
	instance, err := service.repository.GetInstance(ctx, userID, localOrder.InstanceID, true)
	if err != nil {
		return localOrder, err
	}
	// remoteOrder 是供应站对同一外部单号的重新提交结果。
	remoteOrder, err := service.gateway.Buy(ctx, instance, request)
	if err != nil {
		_ = service.repository.RecordOrderError(ctx, userID, localOrder.ExternalOrderNo, err.Error())
		return localOrder, err
	}
	return service.repository.ApplyRemoteOrder(ctx, userID, localOrder.InstanceID, localOrder.ExternalOrderNo, remoteOrder)
}

// MarkTerminalRetryRequired 标记已取消或退款订单需要人工确认后才能创建替代采购单。
func (service *Service) MarkTerminalRetryRequired(ctx context.Context, userID int64, externalOrderNo string) (Order, error) {
	if err := service.repository.RecordOrderError(ctx, userID, externalOrderNo, terminalRetryMarker); err != nil { // err 是终态人工重试标记的持久化错误。
		return Order{}, err
	}
	return service.repository.GetOrder(ctx, userID, externalOrderNo)
}

// TerminalRetryRequired 判断终态订单是否已经等待用户确认重新采购。
func TerminalRetryRequired(order Order) bool {
	return strings.Contains(order.ErrorMessage, terminalRetryMarker)
}

// ReplaceTerminalOrder 为已明确取消或退款的订单创建带递增后缀的新采购单，避免复用终态远程单号。
func (service *Service) ReplaceTerminalOrder(ctx context.Context, userID int64, request PurchaseRequest) (Order, error) {
	// baseOrder 是首次采购对应的本地订单。
	baseOrder, err := service.repository.GetOrder(ctx, userID, strings.TrimSpace(request.ExternalOrderNo))
	if err != nil {
		return Order{}, err
	}
	if !terminalOrderState(baseOrder.State) || !TerminalRetryRequired(baseOrder) {
		return baseOrder, errors.New("原订单尚未进入可替代采购状态")
	}
	// revision 从 2 开始生成稳定的替代订单后缀。
	for revision := 2; revision <= 20; revision++ {
		// replacement 是保留商品、数量、保护价和直充参数的新采购请求。
		replacement := request
		replacement.ExternalOrderNo = fmt.Sprintf("%s-r%d", request.ExternalOrderNo, revision)
		// localOrder、created 表示替代单是否在本次首次创建。
		localOrder, created, createErr := service.repository.CreateOrder(ctx, userID, replacement)
		if createErr != nil {
			return Order{}, createErr
		}
		if !created {
			if terminalOrderState(localOrder.State) {
				continue
			}
			return localOrder, nil
		}
		// instance 是替代采购使用的同一货源实例。
		instance, instanceErr := service.repository.GetInstance(ctx, userID, replacement.InstanceID, true)
		if instanceErr != nil {
			return localOrder, instanceErr
		}
		// remoteOrder 是替代单首次提交后的远程结果。
		remoteOrder, buyErr := service.gateway.Buy(ctx, instance, replacement)
		if buyErr != nil {
			_ = service.repository.RecordOrderError(ctx, userID, replacement.ExternalOrderNo, buyErr.Error())
			return localOrder, buyErr
		}
		return service.repository.ApplyRemoteOrder(ctx, userID, replacement.InstanceID, replacement.ExternalOrderNo, remoteOrder)
	}
	return baseOrder, errors.New("替代采购次数已达到上限")
}

// terminalOrderState 判断履约状态是否已经不可继续处理。
func terminalOrderState(state string) bool {
	return state == "cancelled" || state == "refunded"
}

// ApplyOrderCallback 验证公开回调签名后按实例和外部单号更新状态。
func (service *Service) ApplyOrderCallback(ctx context.Context, publicID string, body []byte) error {
	// instance 是回调路由中公开 ID 对应的秘钥视图。
	instance, err := service.repository.GetInstanceByPublicID(ctx, strings.TrimSpace(publicID), true)
	if err != nil {
		return err
	}
	// remoteOrder 只在签名正确后才会返回。
	remoteOrder, err := service.gateway.VerifyOrderCallback(instance, body)
	if err != nil {
		return err
	}
	_, err = service.repository.ApplyRemoteOrder(ctx, instance.UserID, instance.ID, remoteOrder.ExternalOrderNo, remoteOrder)
	return err
}

// ListOrders 返回最新的外部履约订单。
func (service *Service) ListOrders(ctx context.Context, userID int64, limit int) ([]Order, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return service.repository.ListOrders(ctx, userID, limit)
}

// validateInstanceInput 校验站点协议、地址和身份字段。
func validateInstanceInput(input InstanceInput, allowEmptyKey bool) error {
	if input.Provider != ProviderKasushouV2 && input.Provider != ProviderKayixinV3 && input.Provider != ProviderMifengV1 {
		return fmt.Errorf("不支持的货源协议: %s", input.Provider)
	}
	if input.Name == "" || input.BaseURL == "" {
		return errors.New("货源实例缺少名称或站点地址")
	}
	if input.MerchantUserID == "" {
		if input.Provider == ProviderKayixinV3 || input.Provider == ProviderMifengV1 {
			if input.Provider == ProviderMifengV1 {
				return errors.New("蜜蜂汇云货源实例缺少 AppKey")
			}
			return errors.New("卡易信货源实例缺少 APP ID")
		}
		return errors.New("卡速售货源实例缺少 UserId")
	}
	if !allowEmptyKey && input.APIKey == "" {
		if input.Provider == ProviderKayixinV3 || input.Provider == ProviderMifengV1 {
			if input.Provider == ProviderMifengV1 {
				return errors.New("蜜蜂汇云货源实例缺少 AppSecret")
			}
			return errors.New("卡易信货源实例缺少 AppSecret")
		}
		return errors.New("卡速售货源实例缺少 API Key")
	}
	// parsedURL 是经过标准库校验的站点根地址。
	parsedURL, err := url.Parse(input.BaseURL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
		return errors.New("货源站点地址必须是完整的 HTTP(S) URL")
	}
	return nil
}
