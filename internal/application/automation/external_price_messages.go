package automation

import (
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"
)

// externalPriceMessageDraft 是付款规则配置中与实时跟价消息有关的保存字段。
type externalPriceMessageDraft struct {
	// GuidanceEnabled 表示是否在买家首次咨询指定商品时发送引导。
	GuidanceEnabled bool `json:"price_guidance_enabled"`
	// QueryPromptText 是读取货源实时报价前发送的等待提示。
	QueryPromptText string `json:"price_query_prompt_text"`
	// GuidanceText 是咨询引导文案。
	GuidanceText string `json:"price_guidance_text"`
	// AdjustedNoticeEnabled 表示是否在实时改价成功后通知最终价格。
	AdjustedNoticeEnabled bool `json:"price_adjusted_notice_enabled"`
	// AdjustedNoticeText 是改价成功通知文案。
	AdjustedNoticeText string `json:"price_adjusted_notice_text"`
}

// validateExternalPriceMessageConfig 校验消息只能依附商品级外部跟价规则，且聊天文案不超过平台安全长度。
func validateExternalPriceMessageConfig(raw, triggerType, itemID string, dynamicPriceEnabled bool) error {
	// config 是从规则扩展 JSON 中提取的跟价消息开关和文案。
	var config externalPriceMessageDraft
	if unmarshalErr := json.Unmarshal([]byte(raw), &config); unmarshalErr != nil { // unmarshalErr 是已通过对象校验后仍无法解码字段类型的原因。
		return errors.New("实时跟价消息配置格式无效")
	}
	if !config.GuidanceEnabled && !config.AdjustedNoticeEnabled {
		return nil
	}
	if triggerType != TriggerOrderPaid || !dynamicPriceEnabled {
		return errors.New("咨询引导和改价通知只能用于已开启待付款实时跟价的付款后发货规则")
	}
	if strings.TrimSpace(itemID) == "" {
		return errors.New("开启咨询引导必须关联具体闲鱼商品")
	}
	if utf8.RuneCountInString(config.QueryPromptText) > 1000 || utf8.RuneCountInString(config.GuidanceText) > 1000 || utf8.RuneCountInString(config.AdjustedNoticeText) > 1000 {
		return errors.New("咨询引导和改价通知文案不能超过 1000 个字")
	}
	return nil
}
