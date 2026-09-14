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
	// SuccessNoticeText 是外部采购成功后的可配置交付文案。
	SuccessNoticeText string `json:"fulfillment_success_notice_text"`
}

// withDefaultExternalPriceMessages 为新建的外部货源跟价规则补齐默认开启的询价和改价通知；显式 false 始终保留。
func withDefaultExternalPriceMessages(raw string) (string, error) {
	// config 保留规则已有扩展字段，只在目标开关缺失时写入默认值。
	var config map[string]any
	if decodeErr := json.Unmarshal([]byte(raw), &config); decodeErr != nil || config == nil { // decodeErr 表示规则扩展配置不是可合并的 JSON 对象。
		return "", errors.New("实时跟价消息配置格式无效")
	}
	if _, exists := config["price_guidance_enabled"]; !exists { // exists 区分“未设置”和用户明确关闭。
		config["price_guidance_enabled"] = true
	}
	if _, exists := config["price_adjusted_notice_enabled"]; !exists { // exists 区分“未设置”和用户明确关闭。
		config["price_adjusted_notice_enabled"] = true
	}
	// encoded 是保留原有字段并补齐默认开关后的稳定 JSON。
	encoded, encodeErr := json.Marshal(config)
	if encodeErr != nil {
		return "", encodeErr
	}
	return string(encoded), nil
}

// validateExternalPriceMessageConfig 校验询价消息和采购失败提示的规则范围，并限制聊天文案长度。
func validateExternalPriceMessageConfig(raw, triggerType, itemID string, dynamicPriceEnabled, externalFulfillmentEnabled bool) error {
	// config 是从规则扩展 JSON 中提取的跟价消息开关和文案。
	var config externalPriceMessageDraft
	if unmarshalErr := json.Unmarshal([]byte(raw), &config); unmarshalErr != nil { // unmarshalErr 是已通过对象校验后仍无法解码字段类型的原因。
		return errors.New("实时跟价消息配置格式无效")
	}
	// successNoticeConfigured 表示用户已经保存外部履约成功文案；空值由运行时使用默认模板。
	successNoticeConfigured := strings.TrimSpace(config.SuccessNoticeText) != ""
	// successNoticeActive 只在规则仍包含外部货源动作时参与校验，切回本地库存不会被历史字段阻塞。
	successNoticeActive := successNoticeConfigured && externalFulfillmentEnabled
	if !config.GuidanceEnabled && !config.AdjustedNoticeEnabled && !config.FailureNoticeEnabled && !successNoticeActive {
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
	if successNoticeActive && !strings.Contains(config.SuccessNoticeText, "{delivery_content}") {
		return errors.New("履约成功文案必须包含 {delivery_content}")
	}
	if strings.TrimSpace(itemID) == "" {
		return errors.New("开启外部货源买家通知必须关联具体闲鱼商品")
	}
	if utf8.RuneCountInString(config.QueryPromptText) > 1000 || utf8.RuneCountInString(config.GuidanceText) > 1000 ||
		utf8.RuneCountInString(config.AdjustedNoticeText) > 1000 || utf8.RuneCountInString(config.SafePriceFailureNoticeText) > 1000 ||
		utf8.RuneCountInString(config.FailureNoticeText) > 1000 || utf8.RuneCountInString(config.SuccessNoticeText) > 1000 {
		return errors.New("外部货源买家通知文案不能超过 1000 个字")
	}
	return nil
}
