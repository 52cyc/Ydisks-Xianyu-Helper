package automation

import (
	"errors"

	"xianyu-go/internal/db"
)

// actionFailureRetryMessage 为动作失败选择恢复标记，区分供应站等待、永久拒绝和明确未执行；actionErr 是动作错误，externalFailure 是可选外部采购分类。
func actionFailureRetryMessage(actionErr error, externalFailure *externalFulfillmentActionError) string {
	if externalFailure != nil && externalFailure.noRetry {
		return db.NoRetryErrorPrefix + actionErr.Error()
	}
	if errors.Is(actionErr, errExternalFulfillmentPending) {
		return db.ExternalWaitErrorPrefix + actionErr.Error()
	}
	if errors.Is(actionErr, ErrMessageNotSent) || errors.Is(actionErr, errActionNotPerformed) {
		return db.SafeRetryErrorPrefix + actionErr.Error()
	}
	return actionErr.Error()
}
