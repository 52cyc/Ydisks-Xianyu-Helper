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
	// FailureNoticeEnabled 表示外部采购重试耗尽后是否向买家发送人工处理提示。
	FailureNoticeEnabled bool `json:"fulfillment_failure_notice_enabled"`
	// SafePriceFailureNoticeText 是保护价拦截后携带最新买家售价的重新下单提示。
	SafePriceFailureNoticeText string `json:"fulfillment_safe_price_notice_text"`
	// FailureNoticeText 是不包含货源成本和保护价的最终失败提示。
	FailureNoticeText string `json:"fulfillment_failure_notice_text"`
}

// validateExternalPriceMessageConfig 校验询价消息和采购失败提示的规则范围，并限制聊天文案长度。
func validateExternalPriceMessageConfig(raw, triggerType, itemID string, dynamicPriceEnabled, externalFulfillmentEnabled bool) error {
	// config 是从规则扩展 JSON 中提取的跟价消息开关和文案。
	var config externalPriceMessageDraft
	if unmarshalErr := json.Unmarshal([]byte(raw), &config); unmarshalErr != nil { // unmarshalErr 是已通过对象校验后仍无法解码字段类型的原因。
		return errors.New("实时跟价消息配置格式无效")
	}
	if !config.GuidanceEnabled && !config.AdjustedNoticeEnabled && !config.FailureNoticeEnabled {
		return nil
	}
	if triggerType != TriggerOrderPaid {
		return errors.New("外部货源买家通知只能用于付款后发货规则")
	}
	if (config.GuidanceEnabled || config.AdjustedNoticeEnabled) && !dynamicPriceEnabled {
		return errors.New("咨询引导和改价通知只能用于已开启待付款实时跟价的付款后发货规则")
	}
	if config.FailureNoticeEnabled && !externalFulfillmentEnabled {
		return errors.New("采购失败通知只能用于外部货源发货规则")
	}
	if strings.TrimSpace(itemID) == "" {
		return errors.New("开启外部货源买家通知必须关联具体闲鱼商品")
	}
	if utf8.RuneCountInString(config.QueryPromptText) > 1000 || utf8.RuneCountInString(config.GuidanceText) > 1000 ||
		utf8.RuneCountInString(config.AdjustedNoticeText) > 1000 || utf8.RuneCountInString(config.SafePriceFailureNoticeText) > 1000 ||
		utf8.RuneCountInString(config.FailureNoticeText) > 1000 {
		return errors.New("外部货源买家通知文案不能超过 1000 个字")
	}
	return nil
}
