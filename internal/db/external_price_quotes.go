package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ExternalPriceQuote 保存一次待付款跟价中单个外部发货动作的报价快照。
// 金额字段统一使用整数分；DynamicSafePrice 使用货源协议接收的十进制元字符串。
type ExternalPriceQuote struct {
	// OrderID 是闲鱼订单号，同一订单的多个外部动作共享该值。
	OrderID string
	// CookieID 是闲鱼账号标识，用于隔离和审计报价来源。
	CookieID string
	// ActionID 是付款后发货规则中的外部货源动作标识。
	ActionID int64
	// UnitCostCents 是拍下时查询到的单件货源成本，单位为分。
	UnitCostCents int64
	// FulfillmentQuantity 是该动作结合购买数量后的实际采购件数。
	FulfillmentQuantity int
	// FixedMarkupCents 是管理员配置的每件固定加价，单位为分。
	FixedMarkupCents int64
	// MinimumProfitCents 是付款采购时每件必须保留的最低利润，单位为分。
	MinimumProfitCents int64
	// TargetOrderCents 是本次所有动态动作合计后的闲鱼订单目标总价，单位为分。
	TargetOrderCents int64
	// DynamicSafePrice 是改价成功后该动作采购时使用的订单总保护价。
	DynamicSafePrice string
	// Status 是 pending、adjusted 或 failed，只有 adjusted 可以覆盖规则固定保护价。
	Status string
	// ErrorMessage 保存改价明确失败或结果未知的非敏感原因。
	ErrorMessage string
}

// ExternalPriceQuoteState 返回订单是否已有报价以及报价状态；不存在时 exists 为 false。
func (a *AutomationRules) ExternalPriceQuoteState(ctx context.Context, orderID string) (exists bool, status string, err error) {
	// rowStatus 是同一订单首条报价状态；一次报价的所有动作由同一事务同步收口。
	var rowStatus string
	err = a.DB.QueryRowContext(ctx, `SELECT status FROM external_price_quotes WHERE order_id=? ORDER BY id LIMIT 1`, orderID).Scan(&rowStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	return true, rowStatus, nil
}

// ExternalPriceQuoteTarget 返回订单级跟价快照的目标总价和终态，供重复事件补发买家通知。
func (a *AutomationRules) ExternalPriceQuoteTarget(ctx context.Context, orderID string) (targetCents int64, status string, err error) {
	// queryErr 是订单报价不存在或数据库读取失败的原因。
	queryErr := a.DB.QueryRowContext(ctx, `SELECT target_order_cents,status FROM external_price_quotes WHERE order_id=? ORDER BY id LIMIT 1`, orderID).Scan(&targetCents, &status)
	return targetCents, status, queryErr
}

// CreateExternalPriceQuotes 原子创建一笔订单的全部动作报价；已有报价时返回 created=false，禁止重复改价。
func (a *AutomationRules) CreateExternalPriceQuotes(ctx context.Context, quotes []ExternalPriceQuote) (created bool, err error) {
	if len(quotes) == 0 || quotes[0].OrderID == "" {
		return false, errors.New("外部跟价报价不能为空")
	}
	// tx 保证同一订单多个动作的成本、利润和动态保护价一起落库。
	tx, beginErr := a.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return false, beginErr
	}
	defer tx.Rollback()
	// existingCount 用于在任何远端改价前拒绝重复订单事件。
	var existingCount int
	if queryErr := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM external_price_quotes WHERE order_id=?`, quotes[0].OrderID).Scan(&existingCount); queryErr != nil { // queryErr 是事务内重复报价检查结果。
		return false, queryErr
	}
	if existingCount > 0 {
		return false, nil
	}
	// quote 是当前待写入的外部货源动作报价。
	for _, quote := range quotes {
		if quote.OrderID != quotes[0].OrderID || quote.ActionID <= 0 || quote.FulfillmentQuantity <= 0 {
			return false, errors.New("外部跟价报价字段无效")
		}
		// insertErr 是当前动作报价落库结果。
		if _, insertErr := tx.ExecContext(ctx, `INSERT INTO external_price_quotes
(order_id,cookie_id,action_id,unit_cost_cents,fulfillment_quantity,fixed_markup_cents,minimum_profit_cents,target_order_cents,dynamic_safe_price,status,error_message)
VALUES(?,?,?,?,?,?,?,?,?,'pending','')`, quote.OrderID, quote.CookieID, quote.ActionID, quote.UnitCostCents,
			quote.FulfillmentQuantity, quote.FixedMarkupCents, quote.MinimumProfitCents, quote.TargetOrderCents, quote.DynamicSafePrice); insertErr != nil {
			// rollbackErr 主动释放当前事务，使并发创建者的提交结果可以被后续查询观察。
			rollbackErr := tx.Rollback()
			// concurrentCount 是事务外重新读取的同订单报价数；大于零表示本次只输掉并发唯一键竞争。
			var concurrentCount int
			// queryErr 是并发冲突确认查询结果；查询失败时与原写入错误一起返回。
			queryErr := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM external_price_quotes WHERE order_id=?`, quotes[0].OrderID).Scan(&concurrentCount)
			if queryErr == nil && concurrentCount > 0 {
				return false, nil
			}
			return false, errors.Join(fmt.Errorf("保存外部跟价报价: %w", insertErr), rollbackErr, queryErr)
		}
	}
	if commitErr := tx.Commit(); commitErr != nil { // commitErr 表示整笔订单报价提交是否成功。
		return false, commitErr
	}
	return true, nil
}

// ReplaceExternalPriceQuotesAsAdjusted 为直接付款订单保存已通过倒挂校验的动作级采购上限。
// 仅允许替换尚未成功改价的历史报价；已 adjusted 的订单保持原快照，避免采购过程中改变边界。
func (a *AutomationRules) ReplaceExternalPriceQuotesAsAdjusted(ctx context.Context, quotes []ExternalPriceQuote) error {
	if len(quotes) == 0 || quotes[0].OrderID == "" {
		return errors.New("直接付款外部报价不能为空")
	}
	tx, beginErr := a.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return beginErr
	}
	defer tx.Rollback()
	var adjustedCount int
	if queryErr := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM external_price_quotes WHERE order_id=? AND status='adjusted'`, quotes[0].OrderID).Scan(&adjustedCount); queryErr != nil {
		return queryErr
	}
	if adjustedCount > 0 {
		return tx.Commit()
	}
	if _, deleteErr := tx.ExecContext(ctx, `DELETE FROM external_price_quotes WHERE order_id=?`, quotes[0].OrderID); deleteErr != nil {
		return deleteErr
	}
	for _, quote := range quotes {
		if quote.OrderID != quotes[0].OrderID || quote.ActionID <= 0 || quote.FulfillmentQuantity <= 0 {
			return errors.New("直接付款外部报价字段无效")
		}
		if _, insertErr := tx.ExecContext(ctx, `INSERT INTO external_price_quotes
(order_id,cookie_id,action_id,unit_cost_cents,fulfillment_quantity,fixed_markup_cents,minimum_profit_cents,target_order_cents,dynamic_safe_price,status,error_message)
VALUES(?,?,?,?,?,?,?,?,?,'adjusted','')`, quote.OrderID, quote.CookieID, quote.ActionID, quote.UnitCostCents,
			quote.FulfillmentQuantity, quote.FixedMarkupCents, quote.MinimumProfitCents, quote.TargetOrderCents, quote.DynamicSafePrice); insertErr != nil {
			return insertErr
		}
	}
	return tx.Commit()
}

// FinishExternalPriceQuotes 将订单全部报价收口为 adjusted 或 failed；错误原因不得包含货源凭证。
func (a *AutomationRules) FinishExternalPriceQuotes(ctx context.Context, orderID, status, errorMessage string) error {
	if status != "adjusted" && status != "failed" {
		return errors.New("外部跟价报价终态无效")
	}
	// result 用于确认订单报价确实存在，避免静默丢失改价状态。
	result, updateErr := a.DB.ExecContext(ctx, `UPDATE external_price_quotes SET status=?,error_message=?,updated_at=CURRENT_TIMESTAMP WHERE order_id=? AND status='pending'`, status, errorMessage, orderID)
	if updateErr != nil {
		return updateErr
	}
	// affected 是本次同步收口的动作报价行数。
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil {
		return affectedErr
	}
	if affected == 0 {
		return errors.New("外部跟价报价不存在或已经收口")
	}
	return nil
}

// AdjustedExternalSafePrice 返回改价明确成功后该动作的动态保护价；其他状态必须回退规则固定保护价。
func (a *AutomationRules) AdjustedExternalSafePrice(ctx context.Context, orderID string, actionID int64) (safePrice string, ok bool, err error) {
	// storedSafePrice 是订单级报价为当前发货动作计算的采购总保护价。
	var storedSafePrice string
	err = a.DB.QueryRowContext(ctx, `SELECT dynamic_safe_price FROM external_price_quotes WHERE order_id=? AND action_id=? AND status='adjusted'`, orderID, actionID).Scan(&storedSafePrice)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return storedSafePrice, true, nil
}

// ExternalFulfillmentOrderExists 判断稳定外部单号是否已创建本地履约记录。
// 恢复执行命中时必须继续查原单，不能再用新的实时价格中断已受理订单。
func (a *AutomationRules) ExternalFulfillmentOrderExists(ctx context.Context, userID int64, externalOrderNo string) (bool, error) {
	// count 是当前用户和稳定外部单号匹配的履约记录数。
	var count int
	// err 是履约记录存在性查询错误。
	err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fulfillment_orders WHERE user_id=? AND external_order_no=?`, userID, externalOrderNo).Scan(&count)
	return count > 0, err
}
