package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// RechargeChatIntake 保存一个直充字段的聊天收集和确认状态。
type RechargeChatIntake struct {
	ID          int64
	TaskKey     string
	RunID       int64
	CookieID    string
	ChatID      string
	BuyerID     string
	OrderID     string
	ActionID    int64
	FieldKey    string
	FieldName   string
	ValueSecret string
	MaskedValue string
	Status      string
	PromptSent  bool
}

// SecretOwner 返回绑定任务、动作和字段的加密关联值。
func (i RechargeChatIntake) SecretOwner() string {
	return fmt.Sprintf("%s:%d:%s", i.TaskKey, i.ActionID, i.FieldKey)
}

// EnsureRechargeChatIntake 幂等创建聊天收集记录，已有候选值和状态不会被重置。
func (s *Store) EnsureRechargeChatIntake(ctx context.Context, intake RechargeChatIntake) (*RechargeChatIntake, error) {
	// err 是聊天收集记录的幂等写入错误。
	_, err := s.DB.ExecContext(ctx, `INSERT INTO recharge_chat_intakes
		(task_key,run_id,cookie_id,chat_id,buyer_id,order_id,action_id,field_key,field_name,value_secret,masked_value,status,prompt_sent)
		VALUES(?,?,?,?,?,?,?,?,?,'','','awaiting_input',0)`+dialectUpsert(s.Dialect, []string{"task_key", "action_id", "field_key"}, map[string]string{
		"run_id":     "excluded.run_id",
		"cookie_id":  "excluded.cookie_id",
		"chat_id":    "excluded.chat_id",
		"buyer_id":   "excluded.buyer_id",
		"order_id":   "excluded.order_id",
		"field_name": "excluded.field_name",
	}), intake.TaskKey, intake.RunID, intake.CookieID, intake.ChatID, intake.BuyerID, intake.OrderID, intake.ActionID, intake.FieldKey, intake.FieldName)
	if err != nil {
		return nil, err
	}
	return s.GetRechargeChatIntake(ctx, intake.TaskKey, intake.ActionID, intake.FieldKey)
}

// GetRechargeChatIntake 按稳定动作标识读取直充聊天状态。
func (s *Store) GetRechargeChatIntake(ctx context.Context, taskKey string, actionID int64, fieldKey string) (*RechargeChatIntake, error) {
	return scanRechargeChatIntake(s.DB.QueryRowContext(ctx, `SELECT id,task_key,run_id,cookie_id,chat_id,buyer_id,order_id,action_id,field_key,field_name,value_secret,masked_value,status,prompt_sent
		FROM recharge_chat_intakes WHERE task_key=? AND action_id=? AND field_key=?`, taskKey, actionID, fieldKey))
}

// FindActiveRechargeChatIntake 查找当前会话正在等待输入或确认的最新记录。
func (s *Store) FindActiveRechargeChatIntake(ctx context.Context, cookieID, chatID, buyerID string) (*RechargeChatIntake, error) {
	return scanRechargeChatIntake(s.DB.QueryRowContext(ctx, `SELECT id,task_key,run_id,cookie_id,chat_id,buyer_id,order_id,action_id,field_key,field_name,value_secret,masked_value,status,prompt_sent
		FROM recharge_chat_intakes WHERE cookie_id=? AND chat_id=? AND buyer_id=? AND status IN ('awaiting_input','awaiting_confirm')
		ORDER BY updated_at DESC,id DESC LIMIT 1`, strings.TrimSpace(cookieID), strings.TrimSpace(chatID), strings.TrimSpace(buyerID)))
}

// MarkRechargeChatPromptSent 记录首次索取消息已经成功交给闲鱼传输层。
func (s *Store) MarkRechargeChatPromptSent(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE recharge_chat_intakes SET prompt_sent=1,updated_at=CURRENT_TIMESTAMP WHERE id=?`, id) // err 是索取消息已发送标记的写入错误。
	return err
}

// SaveRechargeChatCandidate 加密保存买家回复，并进入等待确认状态。
func (s *Store) SaveRechargeChatCandidate(ctx context.Context, intake RechargeChatIntake, value, masked string) error {
	// secret 是仅用于数据库静态保存的加密账号。
	secret, err := s.EncryptRechargeChatInput(intake.SecretOwner(), value)
	if err != nil {
		return err
	}
	// result 限制只有当前等待输入的记录能进入确认阶段。
	result, err := s.DB.ExecContext(ctx, `UPDATE recharge_chat_intakes SET value_secret=?,masked_value=?,status='awaiting_confirm',updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='awaiting_input'`, secret, masked, intake.ID) // result、err 是候选账号状态转换的写入结果。
	return requireOneRechargeIntake(result, err)
}

// ResetRechargeChatCandidate 清空候选账号，使买家可重新输入。
func (s *Store) ResetRechargeChatCandidate(ctx context.Context, id int64) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE recharge_chat_intakes SET value_secret='',masked_value='',status='awaiting_input',updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('awaiting_input','awaiting_confirm')`, id) // result、err 是重填状态转换的写入结果。
	return requireOneRechargeIntake(result, err)
}

// ConfirmRechargeChatIntake 原子确认账号并唤醒原自动化任务和运行。
func (s *Store) ConfirmRechargeChatIntake(ctx context.Context, intake RechargeChatIntake) error {
	// tx 保证状态确认和任务唤醒不会只完成一半。
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE recharge_chat_intakes SET status='confirmed',updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='awaiting_confirm'`, intake.ID) // result、err 是确认状态的条件写入结果。
	if err = requireOneRechargeIntake(result, err); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE automation_pending_tasks SET status='pending',attempt_count=0,due_at=0,lease_expires_at=0,error_message='',updated_at=CURRENT_TIMESTAMP WHERE task_key=?`, intake.TaskKey); err != nil {
		return err
	}
	if intake.RunID > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0,next_retry_at=0,updated_at=CURRENT_TIMESTAMP WHERE id=? AND status='running'`, intake.RunID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// scanRechargeChatIntake 统一扫描直充聊天记录并把无行转换为仓储通用错误。
func scanRechargeChatIntake(row *sql.Row) (*RechargeChatIntake, error) {
	// intake 是数据库返回的聊天收集记录。
	var intake RechargeChatIntake
	// promptSent 使用整数兼容 SQLite、MySQL 和 PostgreSQL 的布尔表达。
	var promptSent int
	// err 是数据库行解码错误。
	err := row.Scan(&intake.ID, &intake.TaskKey, &intake.RunID, &intake.CookieID, &intake.ChatID, &intake.BuyerID, &intake.OrderID, &intake.ActionID, &intake.FieldKey, &intake.FieldName, &intake.ValueSecret, &intake.MaskedValue, &intake.Status, &promptSent)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	intake.PromptSent = promptSent != 0
	return &intake, nil
}

// requireOneRechargeIntake 确保状态转换只命中一条正在处理的记录。
func requireOneRechargeIntake(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	// count 是当前状态转换实际更新的行数。
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}
