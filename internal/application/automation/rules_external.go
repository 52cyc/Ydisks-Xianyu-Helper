package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
)

// validateSendCardAction 校验本地卡密或外部货源动作配置。
func (s *RuleService) validateSendCardAction(ctx context.Context, userID int64, draftAction ActionDraft) error {
	sourceType, instanceID, goodsID, configErr := externalFulfillmentConfig(draftAction.ConfigJSON)
	if configErr != nil {
		return configErr
	}
	if sourceType == "external" {
		if instanceID <= 0 || goodsID <= 0 {
			return errors.New("外部货源动作必须选择货源实例并填写商品 ID")
		}
		return nil
	}
	if draftAction.CardID <= 0 {
		return errors.New("发送卡密动作必须选择卡密组")
	}
	card, cardErr := s.ownership.GetCard(ctx, userID, draftAction.CardID)
	if cardErr != nil {
		if !errors.Is(cardErr, ErrRuleNotFound) {
			return cardErr
		}
		return errors.New("卡密组不存在或不属于当前用户")
	}
	if card.Type == "api" && !card.APIReady {
		return errors.New("API 卡券配置无效，请重新保存后再选择")
	}
	return nil
}

// externalFulfillmentConfig 读取发卡动作中的外部货源标识；旧规则默认使用本地卡密。
func externalFulfillmentConfig(raw string) (string, int64, int64, error) {
	var config struct {
		SourceType string `json:"source_type"`
		InstanceID int64  `json:"instance_id"`
		GoodsID    int64  `json:"goods_id"`
	}
	if strings.TrimSpace(raw) == "" {
		return "local", 0, 0, nil
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return "", 0, 0, errors.New("动作配置必须是 JSON 对象")
	}
	config.SourceType = strings.TrimSpace(config.SourceType)
	if config.SourceType == "" || config.SourceType == "local" {
		return "local", config.InstanceID, config.GoodsID, nil
	}
	if config.SourceType != "external" {
		return "", 0, 0, errors.New("发货来源只支持 local 或 external")
	}
	return config.SourceType, config.InstanceID, config.GoodsID, nil
}

// validateExternalPendingPriceConfig 校验外部货源动作中的利润率，返回是否启用价格自动同步。
func validateExternalPendingPriceConfig(raw string) (bool, error) {
	var config struct {
		SourceType          string `json:"source_type"`
		PendingPriceEnabled bool   `json:"pending_price_enabled"`
		PriceSyncEnabled    bool   `json:"price_sync_enabled"`
		ProfitRate          string `json:"profit_rate"`
		FixedMarkup         string `json:"fixed_markup"`
		MinimumProfit       string `json:"minimum_profit"`
	}
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return false, errors.New("动作配置必须是 JSON 对象")
	}
	enabled := config.PriceSyncEnabled || config.PendingPriceEnabled
	if !enabled {
		return false, nil
	}
	if strings.TrimSpace(config.SourceType) != "external" {
		return false, errors.New("价格自动同步只能用于外部货源")
	}
	if strings.TrimSpace(config.ProfitRate) != "" {
		if _, rateErr := parseRuleProfitRateHundredths(config.ProfitRate); rateErr != nil {
			return false, rateErr
		}
		return true, nil
	}
	markupCents, markupErr := parseRuleMoneyCents(config.FixedMarkup, false)
	if markupErr != nil {
		return false, errors.New("固定加价必须是 0.01 到 1000000 元、最多两位小数")
	}
	profitCents, profitErr := parseRuleMoneyCents(config.MinimumProfit, true)
	if profitErr != nil {
		return false, errors.New("最低保留利润必须是 0 到 1000000 元、最多两位小数")
	}
	if profitCents > markupCents {
		return false, errors.New("最低保留利润不能大于固定加价")
	}
	return true, nil
}

// parseRuleProfitRateHundredths 校验 0 到 1000%、最多两位小数的利润率。
func parseRuleProfitRateHundredths(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, ".")
	if len(parts) > 2 || parts[0] == "" || (len(parts) == 2 && len(parts[1]) > 2) {
		return 0, errors.New("利润率必须是 0 到 1000、最多两位小数的百分比")
	}
	whole, wholeErr := strconv.ParseInt(parts[0], 10, 64)
	if wholeErr != nil || whole < 0 || whole > 1000 {
		return 0, errors.New("利润率必须是 0 到 1000、最多两位小数的百分比")
	}
	fraction := int64(0)
	if len(parts) == 2 && parts[1] != "" {
		fracText := parts[1]
		if len(fracText) == 1 {
			fracText += "0"
		}
		var fracErr error
		fraction, fracErr = strconv.ParseInt(fracText, 10, 64)
		if fracErr != nil {
			return 0, errors.New("利润率必须是 0 到 1000、最多两位小数的百分比")
		}
	}
	if whole == 1000 && fraction > 0 {
		return 0, errors.New("利润率不能超过 1000%")
	}
	return whole*100 + fraction, nil
}

// parseRuleMoneyCents 把规则金额解析为整数分；allowZero 控制零金额是否允许。
func parseRuleMoneyCents(raw string, allowZero bool) (int64, error) {
	raw = strings.TrimSpace(raw)
	wholeText, fracText := raw, ""
	if dot := strings.IndexByte(raw, '.'); dot >= 0 {
		wholeText, fracText = raw[:dot], raw[dot+1:]
	}
	if wholeText == "" || len(fracText) > 2 {
		return 0, errors.New("金额格式无效")
	}
	whole, wholeErr := strconv.ParseInt(wholeText, 10, 64)
	if wholeErr != nil || whole < 0 {
		return 0, errors.New("金额格式无效")
	}
	var fraction int64
	if fracText != "" {
		parsedFraction, fractionErr := strconv.ParseInt(fracText, 10, 64)
		if fractionErr != nil || parsedFraction < 0 {
			return 0, errors.New("金额格式无效")
		}
		fraction = parsedFraction
		if len(fracText) == 1 {
			fraction *= 10
		}
	}
	cents := whole*100 + fraction
	if cents > 100000000 || (!allowZero && cents == 0) {
		return 0, errors.New("金额超出范围")
	}
	return cents, nil
}
