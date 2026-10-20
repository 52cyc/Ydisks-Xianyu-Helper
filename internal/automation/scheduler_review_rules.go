package automation

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// errorString 把可选错误转成持久化诊断文本；空错误保持为空字符串。
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// reviewRequestRuleDue 根据订单发货时间、上次求评时间和规则上限判断本轮是否到期。
func reviewRequestRuleDue(order db.Order, rule db.AutomationRule) bool {
	// cfg 保存已应用兼容默认值的求评调度配置。
	cfg := parseReviewRuleConfig(rule.ConfigJSON)
	if cfg.MaxAttempts > 0 && order.ReviewRequestCount >= cfg.MaxAttempts {
		return false
	}
	// baseRaw 保存本次计时起点的数据库时间文本。
	baseRaw := firstNonEmpty(order.ShippedAt, order.UpdatedAt, order.CreatedAt)
	// shippedAt 固定回溯基点为发货时间，防止停用规则后重新启用时补发积压求评。
	shippedAt := parseDBTime(baseRaw)
	if !shippedAt.IsZero() && time.Since(shippedAt) > time.Duration(cfg.effectiveLookbackHours())*time.Hour {
		return false
	}
	// waitHours 表示从计时起点到本次求评的等待小时数。
	waitHours := cfg.AfterShippedHours
	if order.ReviewRequestCount > 0 && strings.TrimSpace(order.LastReviewRequestAt) != "" {
		baseRaw = order.LastReviewRequestAt
		waitHours = cfg.RepeatIntervalHours
	}
	// base 是按数据库兼容格式解析后的 UTC 计时起点。
	base := parseDBTime(baseRaw)
	if base.IsZero() {
		return false
	}
	return time.Since(base) >= time.Duration(waitHours)*time.Hour
}

// reviewRuleConfig 定义首次求评、重复求评与最大尝试次数。
type reviewRuleConfig struct {
	// AfterShippedHours 是首次求评距离发货的等待小时数。
	AfterShippedHours int
	// RepeatIntervalHours 是重复求评的间隔小时数。
	RepeatIntervalHours int
	// MaxAttempts 限制单笔订单的求评次数。
	MaxAttempts int
	// MaxLookbackHours 是发货回溯小时数，零值使用完整求评流程加一天的窗口。
	MaxLookbackHours int
}

// effectiveLookbackHours 返回求评配置的有效回溯小时数；显式窗口不得小于首次求评延迟。
func (c reviewRuleConfig) effectiveLookbackHours() int {
	// minHours 是走完所有求评次数所需的最短小时数。
	minHours := c.AfterShippedHours + (c.MaxAttempts-1)*c.RepeatIntervalHours
	if c.MaxLookbackHours <= 0 {
		return minHours + 24
	}
	if c.MaxLookbackHours < c.AfterShippedHours {
		return c.AfterShippedHours
	}
	return c.MaxLookbackHours
}

// parseReviewRuleConfig 解析历史规则 JSON，字段缺失或非法时保留安全默认值。
func parseReviewRuleConfig(raw string) reviewRuleConfig {
	// cfg 保存兼容历史规则的默认求评策略。
	cfg := reviewRuleConfig{AfterShippedHours: 72, RepeatIntervalHours: 24, MaxAttempts: 1}
	if strings.TrimSpace(raw) == "" {
		return cfg
	}
	// values 保存未绑定的历史 JSON 字段，仅读取已知调度键。
	var values map[string]any
	if json.Unmarshal([]byte(raw), &values) != nil {
		return cfg
	}
	// value 是新版规则配置的首次求评延迟小时数。
	if value := intFromAny(values["after_shipped_hours"]); value > 0 {
		cfg.AfterShippedHours = value
	}
	// value 是历史兼容键中的首次求评延迟小时数。
	if value := intFromAny(values["first_delay_hours"]); value > 0 {
		cfg.AfterShippedHours = value
	}
	// value 是重复求评之间的间隔小时数。
	if value := intFromAny(values["repeat_interval_hours"]); value > 0 {
		cfg.RepeatIntervalHours = value
	}
	// value 是同一订单允许执行的最大求评次数。
	if value := intFromAny(values["max_attempts"]); value > 0 {
		cfg.MaxAttempts = value
	}
	// value 是显式配置的回溯窗口小时数，非正值保留默认策略。
	if value := intFromAny(values["max_lookback_hours"]); value > 0 {
		cfg.MaxLookbackHours = value
	}
	return cfg
}

// intFromAny 将历史 JSON 数字或字符串转为调度整数，无法识别时返回零。
func intFromAny(value any) int {
	// typedValue 保存历史 JSON 配置的实际数字或字符串类型。
	switch typedValue := value.(type) {
	case float64:
		return int(typedValue)
	case int:
		return typedValue
	case string:
		// parsedValue 是去除空白后解析的十进制配置值；非法文本按零处理。
		parsedValue, _ := strconv.Atoi(strings.TrimSpace(typedValue))
		return parsedValue
	default:
		return 0
	}
}

// parseDBTime 按三方言历史文本格式解析 UTC 时间，无匹配格式时返回零值。
func parseDBTime(raw string) time.Time {
	// layout 是当前尝试的时间格式，顺序从标准高精度到历史无时区文本。
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00", // Postgres TEXT(CURRENT_TIMESTAMP)
		"2006-01-02 15:04:05.999999999Z07",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05Z07",
		"2006-01-02 15:04:05", // SQLite/MySQL 历史值；按既有 UTC 约定解释
	} {
		// parsedTime、parseErr 分别表示当前格式的解析结果和失败原因。
		if parsedTime, parseErr := time.ParseInLocation(layout, strings.TrimSpace(raw), time.UTC); parseErr == nil {
			return parsedTime
		}
	}
	return time.Time{}
}
