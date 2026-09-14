package items

import (
	"html"
	"regexp"
	"strings"
	"unicode"
)

// publishBlockTagPattern 匹配货源详情中常见的块级 HTML 标签，并在移除时保留段落边界。
var publishBlockTagPattern = regexp.MustCompile(`(?i)<\s*/?\s*(?:br|p|div|li|ul|ol|h[1-6]|section|article|table|tr|blockquote)(?:\s[^<>]*)?\s*/?\s*>`)

// publishInlineTagPattern 匹配货源详情中常见的行内 HTML 标签，不会吞掉未知的尖括号业务文本。
var publishInlineTagPattern = regexp.MustCompile(`(?i)<\s*/?\s*(?:span|strong|b|i|em|a|font|small|label|img|td|th|tbody|thead)(?:\s[^<>]*)?\s*/?\s*>`)

// normalizeBatchPublishTitle 将批量商品标题转换为闲鱼允许的单行安全文本。
func normalizeBatchPublishTitle(value string) string {
	// decoded 保存实体解码后的标题，确保编码形式的半角尖括号也会被处理。
	decoded := html.UnescapeString(value)
	// safe 保存替换控制字符和平台禁用符号后的标题。
	safe := strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		switch character {
		case '<':
			return '＜'
		case '>':
			return '＞'
		default:
			return character
		}
	}, decoded)
	return strings.Join(strings.Fields(safe), " ")
}

// normalizeBatchPublishDescription 将货源 HTML 详情转换为保留段落、且不含平台禁用半角符号的纯文本。
func normalizeBatchPublishDescription(value string) string {
	// decoded 保存实体解码后的货源描述。
	decoded := html.UnescapeString(value)
	// withParagraphs 在移除块级标签时保留换行语义。
	withParagraphs := publishBlockTagPattern.ReplaceAllString(decoded, "\n")
	// withoutInlineTags 移除不承载段落语义的常见展示标签。
	withoutInlineTags := publishInlineTagPattern.ReplaceAllString(withParagraphs, "")
	// safe 保存清理控制字符并替换剩余半角尖括号后的描述。
	safe := strings.Map(func(character rune) rune {
		switch {
		case character == '\n':
			return character
		case character == '\r' || character == '\t' || unicode.IsControl(character):
			return ' '
		case character == '<':
			return '＜'
		case character == '>':
			return '＞'
		default:
			return character
		}
	}, withoutInlineTags)
	// normalizedLines 保存去除行内冗余空白和连续空行后的描述段落。
	normalizedLines := make([]string, 0, strings.Count(safe, "\n")+1)
	// line 表示当前待压缩空白的描述行。
	for _, line := range strings.Split(safe, "\n") {
		// normalizedLine 保存当前描述行的单空格形式。
		normalizedLine := strings.Join(strings.Fields(line), " ")
		if normalizedLine == "" {
			continue
		}
		normalizedLines = append(normalizedLines, normalizedLine)
	}
	return strings.Join(normalizedLines, "\n")
}
