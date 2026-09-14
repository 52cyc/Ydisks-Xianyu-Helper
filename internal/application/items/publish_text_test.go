package items

import "testing"

// TestNormalizeBatchPublishText 验证实体、货源 HTML、控制字符和半角尖括号会在批量预检边界统一清理。
func TestNormalizeBatchPublishText(t *testing.T) {
	// title 保存包含实体、制表符和换行的原始货源标题。
	title := normalizeBatchPublishTitle("  套餐&lt;4+4&gt;\t\n 限定  ")
	if title != "套餐＜4+4＞ 限定" {
		t.Fatalf("标题清理结果=%q", title)
	}
	// description 保存包含常见 HTML、实体和未知尖括号文本的原始货源描述。
	description := normalizeBatchPublishDescription("<p>第一段&nbsp;<strong>重点</strong></p><div>第二段<br>规格&lt;VIP&gt;\t说明</div>")
	if description != "第一段 重点\n第二段\n规格＜VIP＞ 说明" {
		t.Fatalf("描述清理结果=%q", description)
	}
}
