package mtop

import "errors"

// IsDefinitePublishRejection 判断最终发布错误是否已经由平台明确拒绝，可在修正输入或完成验证后人工重试。
func IsDefinitePublishRejection(err error) bool {
	// kind、classified 保存统一 MTOP 错误分类及其存在性。
	kind, classified := MTopErrorKindOf(err)
	if classified && (kind == MTopErrorBusiness || kind == MTopErrorRiskVerification) {
		return true
	}
	// publishErr 保存发布接口为了兼容历史调用方返回的专用错误。
	var publishErr *PublishError
	return errors.As(err, &publishErr) && isMTopBusinessRet(publishErr.Ret)
}
