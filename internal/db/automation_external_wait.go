package db

import (
	"errors"
	"strings"
	"time"
)

// ErrAutomationRunActive 表示规则仍有关联运行处于执行、恢复或人工核对阶段，因此不能删除。
var ErrAutomationRunActive = errors.New("规则仍有待处理的自动化运行")

// SafeRetryErrorPrefix 标记当前动作明确没有产生外部副作用，可以从动作游标安全恢复。
const SafeRetryErrorPrefix = "[safe_retry]"

// NoRetryErrorPrefix 标记当前动作明确不应进入自动化恢复队列。
const NoRetryErrorPrefix = "[no_retry]"

// ExternalWaitErrorPrefix 标记供应站已经受理但尚未完成的履约订单；该状态按长退避策略查原单，不占用普通三次失败上限。
const ExternalWaitErrorPrefix = "[external_wait]"

// externalWaitMaxAttempts 是外部履约等待的最大查单尝试版本；配合退避序列可覆盖约两小时，超过后进入人工处理。
const externalWaitMaxAttempts = 17

// externalWaitRetryDelay 根据已经完成的查单次数返回下一次查询间隔，逐步降低对供应站的请求压力。
func externalWaitRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 2 * time.Minute
	case 3:
		return 3 * time.Minute
	case 4:
		return 5 * time.Minute
	default:
		return 10 * time.Minute
	}
}

// isExternalWaitError 识别新版等待标记及旧版本已经耗尽三次重试的处理中记录，保证升级后可自动接续原单轮询。
func isExternalWaitError(message string) bool {
	if strings.HasPrefix(message, ExternalWaitErrorPrefix) {
		return true
	}
	if !strings.HasPrefix(message, SafeRetryErrorPrefix) {
		return false
	}
	return strings.Contains(message, "外部货源订单当前状态为 unpaid") ||
		strings.Contains(message, "外部货源订单当前状态为 waiting") ||
		strings.Contains(message, "外部货源订单当前状态为 processing")
}
