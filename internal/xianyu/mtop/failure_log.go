package mtop

import "log/slog"

// logMTopResponseFailure 记录结构化 MTOP 失败；仅已确认自然结束的改价结果降为 Info，其余失败保持 Error。
func logMTopResponseFailure(logger *slog.Logger, api string, kind MTopErrorKind, status int, ret []string, detail string) {
	// attributes 是各日志等级共用的脱敏诊断字段，避免分支之间产生格式漂移。
	attributes := []any{"api", api, "category", string(kind), "http_status", status, "ret", formatMTopRet(ret), "detail", sanitizeMTopText(detail)}
	if api == "订单改价接口" && isAdjustPriceNaturallyClosedRet(ret) {
		logger.Info("订单状态已结束，改价流程自然收口", attributes...)
		return
	}
	logger.Error("MTOP 响应失败", attributes...)
}
