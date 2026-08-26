package db

import (
	"context"
	"encoding/json"
)

// HasEnabledAdjustPriceRule 判断账号是否存在固定改价或外部货源实时跟价能力，供 AI 议价保存时保持模式互斥。
func (a *AutomationRules) HasEnabledAdjustPriceRule(ctx context.Context, cookieID string) (bool, error) {
	// exists 表示启用规则下是否至少存在一个固定订单改价动作或外部货源动态跟价动作。
	var exists bool
	// err 是自动改价互斥查询失败原因。
	err := a.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM automation_rules r
		JOIN automation_rule_actions action ON action.rule_id=r.id
		WHERE r.cookie_id=? AND r.enabled=1 AND r.deleted_at IS NULL
		  AND action.enabled=1 AND action.action_type='adjust_price'
	)`, cookieID).Scan(&exists)
	if err != nil || exists {
		return exists, err
	}
	// rows 包含启用付款规则下的发货动作配置，用于识别 JSON 内的外部货源动态跟价开关。
	rows, rowsErr := a.DB.QueryContext(ctx, `SELECT action.config_json
		FROM automation_rules r
		JOIN automation_rule_actions action ON action.rule_id=r.id
		WHERE r.cookie_id=? AND r.enabled=1 AND r.deleted_at IS NULL
		  AND r.trigger_type='order_paid' AND action.enabled=1 AND action.action_type='send_card'`, cookieID)
	if rowsErr != nil {
		return false, rowsErr
	}
	defer rows.Close()
	for rows.Next() { // 每次推进到一个可能启用外部跟价的发货动作。
		// rawConfig 是动作扩展配置 JSON。
		var rawConfig string
		// config 只读取跟价开关，不解析或暴露货源凭证字段。
		var config struct {
			PendingPriceEnabled bool `json:"pending_price_enabled"`
		}
		if scanErr := rows.Scan(&rawConfig); scanErr != nil { // scanErr 是当前动作配置读取失败原因。
			return false, scanErr
		}
		if json.Unmarshal([]byte(rawConfig), &config) == nil && config.PendingPriceEnabled {
			return true, nil
		}
	}
	return false, rows.Err()
}
