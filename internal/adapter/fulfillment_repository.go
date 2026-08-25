package adapter

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	fulfillmentapp "xianyu-go/internal/application/fulfillment"
	"xianyu-go/internal/db"
)

// FulfillmentRepository 把外部履约应用窄端口适配到当前数据库。
type FulfillmentRepository struct {
	// store 提供数据库和用途隔离的秘密编解码能力。
	store *db.Store
}

// NewFulfillmentRepository 创建外部履约仓储适配器。
func NewFulfillmentRepository(store *db.Store) *FulfillmentRepository {
	return &FulfillmentRepository{store: store}
}

// ListInstances 列出用户货源实例，不解密 API 密钥。
func (repository *FulfillmentRepository) ListInstances(ctx context.Context, userID int64) ([]fulfillmentapp.Instance, error) {
	// rows 是按创建时间倒序返回的实例游标。
	rows, err := repository.store.DB.QueryContext(ctx, `SELECT id, public_id, user_id, name, provider, base_url, merchant_user_id,
		CASE WHEN api_key='' THEN 0 ELSE 1 END, capabilities_json, enabled, created_at, updated_at
		FROM fulfillment_instances WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// instances 是转换后的非敏感实例列表。
	instances := make([]fulfillmentapp.Instance, 0)
	for rows.Next() {
		// instance 是当前行的实例摘要。
		instance, scanErr := scanInstance(rows, false, repository.store)
		if scanErr != nil {
			return nil, scanErr
		}
		instances = append(instances, instance)
	}
	return instances, rows.Err()
}

// GetInstance 按用户归属读取实例，withSecret 仅供发起外部请求时使用。
func (repository *FulfillmentRepository) GetInstance(ctx context.Context, userID, instanceID int64, withSecret bool) (fulfillmentapp.Instance, error) {
	// row 是同时受用户 ID 和实例 ID 约束的唯一记录。
	row := repository.store.DB.QueryRowContext(ctx, `SELECT id, public_id, user_id, name, provider, base_url, merchant_user_id,
		api_key, capabilities_json, enabled, created_at, updated_at FROM fulfillment_instances WHERE id=? AND user_id=?`, instanceID, userID)
	return scanSecretInstance(row, withSecret, repository.store)
}

// GetInstanceByPublicID 仅为签名回调按不可枚举的公开 ID 读取实例。
func (repository *FulfillmentRepository) GetInstanceByPublicID(ctx context.Context, publicID string, withSecret bool) (fulfillmentapp.Instance, error) {
	// row 是公开回调 ID 对应的唯一实例。
	row := repository.store.DB.QueryRowContext(ctx, `SELECT id, public_id, user_id, name, provider, base_url, merchant_user_id,
		api_key, capabilities_json, enabled, created_at, updated_at FROM fulfillment_instances WHERE public_id=?`, publicID)
	return scanSecretInstance(row, withSecret, repository.store)
}

// CreateInstance 加密 API 密钥后创建多实例货源配置。
func (repository *FulfillmentRepository) CreateInstance(ctx context.Context, userID int64, input fulfillmentapp.InstanceInput) (fulfillmentapp.Instance, error) {
	// publicID 是公开回调地址使用的随机不可枚举标识。
	publicID, err := randomPublicID()
	if err != nil {
		return fulfillmentapp.Instance{}, err
	}
	// encryptedKey 是以 publicID 作为 AAD owner 的 API 密钥密文。
	encryptedKey, err := repository.store.EncryptFulfillmentAPIKey(publicID, input.APIKey)
	if err != nil {
		return fulfillmentapp.Instance{}, err
	}
	// capabilitiesJSON 是实例能力开关的稳定 JSON。
	capabilitiesJSON, _ := json.Marshal(input.Capabilities)
	// instanceID 是跨方言插入后返回的新实例主键。
	instanceID, err := db.InsertReturningID(ctx, repository.store.DB, repository.store.Dialect, `INSERT INTO fulfillment_instances
		(public_id,user_id,name,provider,base_url,merchant_user_id,api_key,capabilities_json,enabled)
		VALUES(?,?,?,?,?,?,?,?,?)`, publicID, userID, input.Name, input.Provider, input.BaseURL, input.MerchantUserID, encryptedKey, string(capabilitiesJSON), input.Enabled)
	if err != nil {
		return fulfillmentapp.Instance{}, fmt.Errorf("%w: %v", fulfillmentapp.ErrConflict, err)
	}
	return repository.GetInstance(ctx, userID, instanceID, false)
}

// UpdateInstance 更新实例，空 APIKey 保留旧密钥。
func (repository *FulfillmentRepository) UpdateInstance(ctx context.Context, userID, instanceID int64, input fulfillmentapp.InstanceInput) (fulfillmentapp.Instance, error) {
	// existing 提供 API 密钥加密所需的稳定 publicID。
	existing, err := repository.GetInstance(ctx, userID, instanceID, false)
	if err != nil {
		return fulfillmentapp.Instance{}, err
	}
	// capabilitiesJSON 是更新后的能力开关 JSON。
	capabilitiesJSON, _ := json.Marshal(input.Capabilities)
	// result 是带用户归属限制的更新结果。
	var result sql.Result
	if input.APIKey == "" {
		result, err = repository.store.DB.ExecContext(ctx, `UPDATE fulfillment_instances SET name=?,provider=?,base_url=?,merchant_user_id=?,capabilities_json=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`, input.Name, input.Provider, input.BaseURL, input.MerchantUserID, string(capabilitiesJSON), input.Enabled, instanceID, userID)
	} else {
		// encryptedKey 是使用原 publicID 重新加密的新密钥。
		encryptedKey, encryptErr := repository.store.EncryptFulfillmentAPIKey(existing.PublicID, input.APIKey)
		if encryptErr != nil {
			return fulfillmentapp.Instance{}, encryptErr
		}
		result, err = repository.store.DB.ExecContext(ctx, `UPDATE fulfillment_instances SET name=?,provider=?,base_url=?,merchant_user_id=?,api_key=?,capabilities_json=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`, input.Name, input.Provider, input.BaseURL, input.MerchantUserID, encryptedKey, string(capabilitiesJSON), input.Enabled, instanceID, userID)
	}
	if err != nil {
		return fulfillmentapp.Instance{}, fmt.Errorf("%w: %v", fulfillmentapp.ErrConflict, err)
	}
	// affected 表示是否真正匹配到当前用户的实例。
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fulfillmentapp.Instance{}, fulfillmentapp.ErrNotFound
	}
	return repository.GetInstance(ctx, userID, instanceID, false)
}

// DeleteInstance 删除当前用户的实例，外键会阻止删除已使用实例。
func (repository *FulfillmentRepository) DeleteInstance(ctx context.Context, userID, instanceID int64) error {
	// result 是带归属限制的删除结果。
	result, err := repository.store.DB.ExecContext(ctx, `DELETE FROM fulfillment_instances WHERE id=? AND user_id=?`, instanceID, userID)
	if err != nil {
		return fmt.Errorf("%w: 实例已被映射或订单使用", fulfillmentapp.ErrConflict)
	}
	// affected 表示是否找到待删除实例。
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fulfillmentapp.ErrNotFound
	}
	return nil
}

// ListMappings 列出用户的商品规格映射。
func (repository *FulfillmentRepository) ListMappings(ctx context.Context, userID int64) ([]fulfillmentapp.Mapping, error) {
	// rows 是当前用户的映射游标。
	rows, err := repository.store.DB.QueryContext(ctx, `SELECT id,user_id,account_id,item_id,spec_name,spec_value,instance_id,remote_goods_id,goods_type,safe_price,quantity_mode,fixed_quantity,attach_mapping_json,enabled,created_at,updated_at FROM fulfillment_mappings WHERE user_id=? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// mappings 是解析 attach 映射后的列表。
	mappings := make([]fulfillmentapp.Mapping, 0)
	for rows.Next() {
		// mapping 是当前行的商品映射。
		var mapping fulfillmentapp.Mapping
		// attachJSON 是附加字段来源映射 JSON。
		var attachJSON string
		if // scanErr 是当前映射行扫描错误。
		scanErr := rows.Scan(&mapping.ID, &mapping.UserID, &mapping.AccountID, &mapping.ItemID, &mapping.SpecName, &mapping.SpecValue, &mapping.InstanceID, &mapping.RemoteGoodsID, &mapping.GoodsType, &mapping.SafePrice, &mapping.QuantityMode, &mapping.FixedQuantity, &attachJSON, &mapping.Enabled, &mapping.CreatedAt, &mapping.UpdatedAt); scanErr != nil {
			return nil, scanErr
		}
		_ = json.Unmarshal([]byte(attachJSON), &mapping.AttachMapping)
		mappings = append(mappings, mapping)
	}
	return mappings, rows.Err()
}

// CreateMapping 创建用户、账号、商品和规格唯一的货源映射。
func (repository *FulfillmentRepository) CreateMapping(ctx context.Context, userID int64, input fulfillmentapp.MappingInput) (fulfillmentapp.Mapping, error) {
	if // instanceErr 是映射所属实例的归属校验错误。
	_, instanceErr := repository.GetInstance(ctx, userID, input.InstanceID, false); instanceErr != nil {
		return fulfillmentapp.Mapping{}, instanceErr
	}
	// attachJSON 是附加字段来源映射的 JSON。
	attachJSON, _ := json.Marshal(input.AttachMapping)
	// mappingID 是跨方言插入后返回的新映射主键。
	mappingID, err := db.InsertReturningID(ctx, repository.store.DB, repository.store.Dialect, `INSERT INTO fulfillment_mappings(user_id,account_id,item_id,spec_name,spec_value,instance_id,remote_goods_id,goods_type,safe_price,quantity_mode,fixed_quantity,attach_mapping_json,enabled) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, userID, input.AccountID, input.ItemID, input.SpecName, input.SpecValue, input.InstanceID, input.RemoteGoodsID, input.GoodsType, input.SafePrice, input.QuantityMode, input.FixedQuantity, string(attachJSON), input.Enabled)
	if err != nil {
		return fulfillmentapp.Mapping{}, fmt.Errorf("%w: %v", fulfillmentapp.ErrConflict, err)
	}
	return fulfillmentapp.Mapping{ID: mappingID, UserID: userID, AccountID: input.AccountID, ItemID: input.ItemID, SpecName: input.SpecName, SpecValue: input.SpecValue, InstanceID: input.InstanceID, RemoteGoodsID: input.RemoteGoodsID, GoodsType: input.GoodsType, SafePrice: input.SafePrice, QuantityMode: input.QuantityMode, FixedQuantity: input.FixedQuantity, AttachMapping: input.AttachMapping, Enabled: input.Enabled}, nil
}

// DeleteMapping 删除当前用户名下的映射。
func (repository *FulfillmentRepository) DeleteMapping(ctx context.Context, userID, mappingID int64) error {
	// result 是带归属限制的删除结果。
	result, err := repository.store.DB.ExecContext(ctx, `DELETE FROM fulfillment_mappings WHERE id=? AND user_id=?`, mappingID, userID)
	if err != nil {
		return err
	}
	// affected 表示是否找到待删除映射。
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fulfillmentapp.ErrNotFound
	}
	return nil
}

// CreateOrder 在外部请求前原子创建幂等订单。
func (repository *FulfillmentRepository) CreateOrder(ctx context.Context, userID int64, request fulfillmentapp.PurchaseRequest) (fulfillmentapp.Order, bool, error) {
	if // instanceErr 是采购实例的归属校验错误。
	_, instanceErr := repository.GetInstance(ctx, userID, request.InstanceID, false); instanceErr != nil {
		return fulfillmentapp.Order{}, false, instanceErr
	}
	// orderID 是跨方言插入后返回的新履约订单主键。
	orderID, err := db.InsertReturningID(ctx, repository.store.DB, repository.store.Dialect, `INSERT INTO fulfillment_orders(user_id,instance_id,external_order_no,xianyu_order_id,remote_goods_id,quantity,status,state) VALUES(?,?,?,?,?,?,0,'created')`, userID, request.InstanceID, request.ExternalOrderNo, request.XianyuOrderID, request.RemoteGoodsID, request.Quantity)
	if err != nil {
		// existing 是外部订单号冲突时返回的原幂等记录。
		existing, getErr := repository.GetOrder(ctx, userID, request.ExternalOrderNo)
		if getErr != nil {
			return fulfillmentapp.Order{}, false, fmt.Errorf("%w: %v", fulfillmentapp.ErrConflict, err)
		}
		if existing.InstanceID != request.InstanceID || existing.RemoteGoodsID != request.RemoteGoodsID || existing.Quantity != request.Quantity {
			return fulfillmentapp.Order{}, false, errors.New("外部订单号已用于不同的采购参数")
		}
		return existing, false, nil
	}
	// createdOrder 是远程请求发起前已持久化的幂等记录。
	createdOrder := fulfillmentapp.Order{ID: orderID, UserID: userID, InstanceID: request.InstanceID, ExternalOrderNo: request.ExternalOrderNo, XianyuOrderID: request.XianyuOrderID, RemoteGoodsID: request.RemoteGoodsID, Quantity: request.Quantity, State: "created"}
	return createdOrder, true, nil
}

// ApplyRemoteOrder 加密卡密和直充结果后更新本地订单。
func (repository *FulfillmentRepository) ApplyRemoteOrder(ctx context.Context, userID, instanceID int64, externalOrderNo string, remote fulfillmentapp.RemoteOrder) (fulfillmentapp.Order, error) {
	// secretJSON 是卡密、充值结果和提示的联合敏感文本。
	secretJSON, _ := json.Marshal(map[string]any{"card_list": remote.CardList, "recharge_info": remote.RechargeInfo, "recharge_hints": remote.RechargeHints})
	// encryptedResult 是以外部订单号作为 AAD owner 的密文。
	encryptedResult, err := repository.store.EncryptFulfillmentResult(externalOrderNo, string(secretJSON))
	if err != nil {
		return fulfillmentapp.Order{}, err
	}
	// state 优先使用协议适配器给出的统一状态，并兼容旧卡速售数字状态。
	state := strings.TrimSpace(remote.State)
	if state == "" {
		state = orderState(remote.Status)
	}
	// result 是带归属限制的订单更新结果。
	result, err := repository.store.DB.ExecContext(ctx, `UPDATE fulfillment_orders SET remote_order_no=?,status=?,state=?,total_price=?,result_secret=?,error_message='',updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND instance_id=? AND external_order_no=?`, remote.RemoteOrderNo, remote.Status, state, remote.TotalPrice, encryptedResult, userID, instanceID, externalOrderNo)
	if err != nil {
		return fulfillmentapp.Order{}, err
	}
	// affected 表示回调或查询是否匹配本地订单。
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fulfillmentapp.Order{}, fulfillmentapp.ErrNotFound
	}
	return repository.GetOrder(ctx, userID, externalOrderNo)
}

// RecordOrderError 保存最近一次远程采购或查单错误，供货源管理页面直接查看。
func (repository *FulfillmentRepository) RecordOrderError(ctx context.Context, userID int64, externalOrderNo, message string) error {
	// result 是带用户归属约束的履约错误更新结果。
	result, err := repository.store.DB.ExecContext(ctx, `UPDATE fulfillment_orders SET error_message=?,updated_at=CURRENT_TIMESTAMP WHERE user_id=? AND external_order_no=?`, strings.TrimSpace(message), userID, strings.TrimSpace(externalOrderNo))
	if err != nil {
		return err
	}
	// affected 表示是否找到对应的本地履约订单。
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fulfillmentapp.ErrNotFound
	}
	return nil
}

// GetOrder 读取并解密当前用户的一笔履约订单。
func (repository *FulfillmentRepository) GetOrder(ctx context.Context, userID int64, externalOrderNo string) (fulfillmentapp.Order, error) {
	// row 是用户和外部订单号唯一确定的履约记录。
	row := repository.store.DB.QueryRowContext(ctx, `SELECT id,user_id,instance_id,external_order_no,remote_order_no,xianyu_order_id,remote_goods_id,quantity,status,state,total_price,result_secret,error_message,created_at,updated_at FROM fulfillment_orders WHERE user_id=? AND external_order_no=?`, userID, externalOrderNo)
	return scanOrder(row, repository.store)
}

// ListOrders 列出当前用户最新履约订单并解密结果。
func (repository *FulfillmentRepository) ListOrders(ctx context.Context, userID int64, limit int) ([]fulfillmentapp.Order, error) {
	// rows 是按主键倒序限制数量的订单游标。
	rows, err := repository.store.DB.QueryContext(ctx, `SELECT id,user_id,instance_id,external_order_no,remote_order_no,xianyu_order_id,remote_goods_id,quantity,status,state,total_price,result_secret,error_message,created_at,updated_at FROM fulfillment_orders WHERE user_id=? ORDER BY id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// orders 是已解密且通过归属约束的订单列表。
	orders := make([]fulfillmentapp.Order, 0)
	for rows.Next() {
		// order 是当前行的履约订单。
		order, scanErr := scanOrder(rows, repository.store)
		if scanErr != nil {
			return nil, scanErr
		}
		orders = append(orders, order)
	}
	return orders, rows.Err()
}

// rowScanner 是 sql.Row 和 sql.Rows 共用的最小扫描端口。
type rowScanner interface {
	Scan(...any) error
}

// scanInstance 扫描不包含 API 密钥值的实例摘要。
func scanInstance(scanner rowScanner, _ bool, _ *db.Store) (fulfillmentapp.Instance, error) {
	// instance 是扫描后的货源实例。
	var instance fulfillmentapp.Instance
	// capabilitiesJSON 是实例可选能力开关。
	var capabilitiesJSON string
	if // scanErr 是非敏感实例行扫描错误。
	scanErr := scanner.Scan(&instance.ID, &instance.PublicID, &instance.UserID, &instance.Name, &instance.Provider, &instance.BaseURL, &instance.MerchantUserID, &instance.HasAPIKey, &capabilitiesJSON, &instance.Enabled, &instance.CreatedAt, &instance.UpdatedAt); scanErr != nil {
		return fulfillmentapp.Instance{}, mapNotFound(scanErr)
	}
	_ = json.Unmarshal([]byte(capabilitiesJSON), &instance.Capabilities)
	return instance, nil
}

// scanSecretInstance 扫描实例并按需解密 API 密钥。
func scanSecretInstance(scanner rowScanner, withSecret bool, store *db.Store) (fulfillmentapp.Instance, error) {
	// instance 是扫描后的货源实例。
	var instance fulfillmentapp.Instance
	// encryptedKey 是数据库中保存的 API 密钥密文。
	var encryptedKey string
	// capabilitiesJSON 是实例可选能力开关。
	var capabilitiesJSON string
	if // scanErr 是含密钥实例行扫描错误。
	scanErr := scanner.Scan(&instance.ID, &instance.PublicID, &instance.UserID, &instance.Name, &instance.Provider, &instance.BaseURL, &instance.MerchantUserID, &encryptedKey, &capabilitiesJSON, &instance.Enabled, &instance.CreatedAt, &instance.UpdatedAt); scanErr != nil {
		return fulfillmentapp.Instance{}, mapNotFound(scanErr)
	}
	instance.HasAPIKey = encryptedKey != ""
	_ = json.Unmarshal([]byte(capabilitiesJSON), &instance.Capabilities)
	if withSecret {
		// plainKey 只在本次应用服务外部请求中短时存活。
		plainKey, err := store.DecryptFulfillmentAPIKey(instance.PublicID, encryptedKey)
		if err != nil {
			return fulfillmentapp.Instance{}, err
		}
		instance.APIKey = plainKey
	}
	return instance, nil
}

// scanOrder 扫描并解密一笔归属已校验的履约订单。
func scanOrder(scanner rowScanner, store *db.Store) (fulfillmentapp.Order, error) {
	// order 是扫描后的履约订单。
	var order fulfillmentapp.Order
	// encryptedResult 是卡密和直充结果的数据库密文。
	var encryptedResult string
	if // scanErr 是履约订单行扫描错误。
	scanErr := scanner.Scan(&order.ID, &order.UserID, &order.InstanceID, &order.ExternalOrderNo, &order.RemoteOrderNo, &order.XianyuOrderID, &order.RemoteGoodsID, &order.Quantity, &order.Status, &order.State, &order.TotalPrice, &encryptedResult, &order.ErrorMessage, &order.CreatedAt, &order.UpdatedAt); scanErr != nil {
		return fulfillmentapp.Order{}, mapNotFound(scanErr)
	}
	if encryptedResult == "" {
		return order, nil
	}
	// plainResult 是已通过归属查询后解密的履约结果 JSON。
	plainResult, err := store.DecryptFulfillmentResult(order.ExternalOrderNo, encryptedResult)
	if err != nil {
		return fulfillmentapp.Order{}, err
	}
	// result 是解密后的卡密和直充结果容器。
	var result struct {
		CardList      []string `json:"card_list"`
		RechargeInfo  string   `json:"recharge_info"`
		RechargeHints string   `json:"recharge_hints"`
	}
	if // decodeErr 是解密后履约结果 JSON 解码错误。
	decodeErr := json.Unmarshal([]byte(plainResult), &result); decodeErr != nil {
		return fulfillmentapp.Order{}, errors.New("履约结果密文内容无效")
	}
	order.CardList, order.RechargeInfo, order.RechargeHints = result.CardList, result.RechargeInfo, result.RechargeHints
	return order, nil
}

// randomPublicID 生成 128 位随机公开回调 ID。
func randomPublicID() (string, error) {
	// raw 是用于公开 ID 的加密安全随机字节。
	raw := make([]byte, 16)
	if // randomErr 是加密安全随机数读取错误。
	_, randomErr := rand.Read(raw); randomErr != nil {
		return "", randomErr
	}
	return hex.EncodeToString(raw), nil
}

// orderState 把卡速售数字状态转为稳定本地状态。
func orderState(status int) string {
	switch status {
	case -1:
		return "unpaid"
	case 1:
		return "waiting"
	case 2:
		return "processing"
	case 3:
		return "succeeded"
	case 4:
		return "cancelled"
	case 5:
		return "refunded"
	default:
		return "unknown"
	}
}

// mapNotFound 把 SQL 空结果转为应用层稳定错误。
func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fulfillmentapp.ErrNotFound
	}
	return err
}
