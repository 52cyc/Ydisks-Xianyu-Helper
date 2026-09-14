package mtop

import (
	"encoding/json"
	"strconv"
	"strings"
)

// classifyPublishError 根据平台返回码和响应体区分凭证失效、库存权限不足及未知发布失败；返回值保留脱敏后的业务诊断供上层分类。
func classifyPublishError(ret []string, decoded map[string]any) error {
	// bodyBytes 是响应对象的 JSON 诊断表示；编码失败时允许退化为空内容，不能覆盖原始平台返回码。
	bodyBytes, _ := json.Marshal(decoded)
	// body 保存不含请求凭证的响应诊断文本，仅用于构造发布错误。
	body := string(bodyBytes)
	// joined 将平台返回码与响应诊断归一为小写文本，供兼容不同版本的关键词分类。
	joined := strings.ToLower(strings.Join(append(ret, body), " "))
	if isTokenExpiredRet(ret) || strings.Contains(joined, "login") || strings.Contains(joined, "session") {
		return &PublishError{Code: PublishErrorTokenExpired, Ret: ret, Body: body}
	}
	// stockTerms 是平台描述库存、多件发布能力时可能出现的中英文关键词。
	stockTerms := []string{"库存", "数量", "多库存", "多件", "quantity", "stock", "inventory"}
	// permissionTerms 是平台拒绝账号使用某项发布能力时可能出现的中英文关键词。
	permissionTerms := []string{"权限", "未开通", "不支持", "没有", "无法", "permission", "forbidden", "not allow", "not support"}
	if containsAny(joined, stockTerms) && containsAny(joined, permissionTerms) {
		return &PublishError{Code: PublishErrorStockPermissionMissing, Ret: ret, Body: body}
	}
	return &PublishError{Code: PublishErrorUnknown, Ret: ret, Body: body}
}

// containsAny 判断归一化文本是否包含任一候选关键词；候选词会转为小写以保持英文匹配一致。
func containsAny(text string, terms []string) bool {
	// term 是当前检查的业务关键词。
	for _, term := range terms {
		if strings.Contains(text, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

// retFromDecoded 从动态 MTOP 响应中读取返回码数组；字段缺失或类型不符时返回空数组。
func retFromDecoded(decoded map[string]any) []string {
	// raw 保存协议响应中的原始返回码元素；类型不匹配时按空集合处理。
	raw, _ := decoded["ret"].([]any)
	// result 保存转换后的平台返回码，顺序与响应保持一致。
	result := make([]string, 0, len(raw))
	// rawRet 是当前转换的单个平台返回码元素。
	for _, rawRet := range raw {
		result = append(result, mtopString(rawRet))
	}
	return result
}

// mapFromAny 将动态协议字段收窄为字符串键对象；非对象输入返回 nil，交由调用方决定兼容或报错。
func mapFromAny(value any) map[string]any {
	// result、ok 分别保存类型收窄后的对象及输入是否符合协议形状。
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return nil
}

// parsePix 解析平台使用的“宽x高”像素文本；格式非法的任一输入返回零宽零高。
func parsePix(pix string) (int, int) {
	// parts 保存按小写 x 分隔的宽高文本。
	parts := strings.Split(pix, "x")
	if len(parts) != 2 {
		return 0, 0
	}
	// width 是忽略转换错误后的宽度像素值；非法数值保持为零。
	width, _ := strconv.Atoi(parts[0])
	// height 是忽略转换错误后的高度像素值；非法数值保持为零。
	height, _ := strconv.Atoi(parts[1])
	return width, height
}

// centsText 将整数分金额转换为固定两位小数的元文本，供闲鱼发布协议字段使用。
func centsText(cents int64) string {
	return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64)
}

// escapeMultipartFilename 转义 multipart 文件名中的反斜杠和双引号，防止破坏 Content-Disposition 参数边界。
func escapeMultipartFilename(filename string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(filename)
}
