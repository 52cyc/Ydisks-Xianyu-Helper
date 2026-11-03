package items

import "strings"

// normalizePostageMode 将用户邮费模式映射为平台使用的稳定值。
func normalizePostageMode(raw string) string {
	// value 保存归一化前的邮费模式文本。
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "包邮", "free_shipping":
		return "free"
	case "固定邮费", "一口价邮费":
		return "fixed"
	case "按距离计费", "distance_fee":
		return "distance"
	case "无需邮寄", "不需要邮寄", "no_shipping":
		return "none"
	default:
		return value
	}
}
