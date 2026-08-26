package db

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ExternalPriceMessageRecord 描述一条咨询引导或改价通知的幂等投递身份。
type ExternalPriceMessageRecord struct {
	// DedupeKey 是跨重启保持稳定的业务防重键，不包含聊天正文。
	DedupeKey string
	// CookieID 是发送消息的闲鱼账号标识。
	CookieID string
	// ChatID 是消息投递到的会话标识。
	ChatID string
	// ItemID 是启用实时跟价的闲鱼商品标识。
	ItemID string
	// RuleID 是产生消息配置的付款后自动化规则主键。
	RuleID int64
	// OrderID 仅在改价成功通知中保存对应闲鱼订单号。
	OrderID string
	// MessageKind 区分首次咨询引导和改价成功通知。
	MessageKind string
}

// ClaimExternalPriceMessage 原子领取一条跟价消息的五分钟发送租约；已发送或未过期的任务不会重复领取。
func (a *AutomationRules) ClaimExternalPriceMessage(ctx context.Context, record ExternalPriceMessageRecord) (bool, error) {
	if a == nil || a.DB == nil {
		return false, errors.New("自动化规则仓储未初始化")
	}
	if strings.TrimSpace(record.DedupeKey) == "" || strings.TrimSpace(record.CookieID) == "" || strings.TrimSpace(record.ChatID) == "" || record.RuleID <= 0 {
		return false, errors.New("外部跟价消息防重字段不完整")
	}
	// now 是判断历史投递租约是否过期的当前 Unix 秒。
	now := time.Now().UTC().Unix()
	// leaseExpiresAt 是本次投递租约的五分钟到期时间，进程退出后允许其他实例接管。
	leaseExpiresAt := now + int64((5*time.Minute)/time.Second)
	// query 使用各数据库方言的忽略冲突语法，让首次领取和并发重复消息保持原子性。
	query := dialectInsertIgnorePrefix(a.Dialect) + ` INTO external_price_message_records
		(dedupe_key,cookie_id,chat_id,item_id,rule_id,order_id,message_kind,status,last_error,lease_expires_at,updated_at)
		VALUES (?,?,?,?,?,?,?,'pending','',?,CURRENT_TIMESTAMP)` + dialectInsertIgnore(a.Dialect, []string{"dedupe_key"})
	// result 和 insertErr 是首次插入的影响行数及数据库错误。
	result, insertErr := a.DB.ExecContext(ctx, query, record.DedupeKey, record.CookieID, record.ChatID, record.ItemID,
		record.RuleID, record.OrderID, record.MessageKind, leaseExpiresAt)
	if insertErr != nil {
		return false, insertErr
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr == nil && affected > 0 { // affected 和 rowsErr 用于确认当前调用是否创建了新投递任务。
		return true, nil
	}
	// retryResult 和 retryErr 尝试接管明确失败或进程崩溃后租约过期的投递任务。
	retryResult, retryErr := a.DB.ExecContext(ctx, `UPDATE external_price_message_records
		SET status='pending',last_error='',lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE dedupe_key=? AND (status='failed' OR (status='pending' AND lease_expires_at<?))`, leaseExpiresAt, record.DedupeKey, now)
	if retryErr != nil {
		return false, retryErr
	}
	// affected 是成功接管的记录数；零表示记录已经发送或仍由其他执行者持有。
	affected, rowsErr := retryResult.RowsAffected()
	return affected > 0, rowsErr
}

// FinishExternalPriceMessage 把投递任务收口为 sent 或 failed；失败摘要不得包含货源凭证。
func (a *AutomationRules) FinishExternalPriceMessage(ctx context.Context, dedupeKey, status, message string) error {
	if status != "sent" && status != "failed" {
		return errors.New("外部跟价消息终态无效")
	}
	// result 和 updateErr 是带 pending 前置条件的状态收口结果。
	result, updateErr := a.DB.ExecContext(ctx, `UPDATE external_price_message_records
		SET status=?,last_error=?,lease_expires_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE dedupe_key=? AND status='pending'`, status, message, dedupeKey)
	if updateErr != nil {
		return updateErr
	}
	// affected 验证租约仍属于当前投递者，避免静默覆盖其他终态。
	affected, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return rowsErr
	}
	if affected != 1 {
		return errors.New("外部跟价消息投递状态已经变化")
	}
	return nil
}
