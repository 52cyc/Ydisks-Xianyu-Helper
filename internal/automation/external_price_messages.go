package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

const (
	// legacyExternalPriceGuidance 是升级前保存的默认咨询引导，用于无感迁移到先报价流程。
	legacyExternalPriceGuidance = "亲，您好～本商品会根据货源最新价格自动报价。请先拍下但不要付款，系统改价完成后会通知您，确认金额后再付款。"
	// defaultExternalPriceQueryPrompt 是买家咨询后、系统读取货源报价前的默认提示。
	defaultExternalPriceQueryPrompt = "亲，您好～正在为您查询当前最新价格，请稍候。"
	// defaultExternalPriceGuidance 是货源查询成功后展示报价和拍下未付款流程的默认文案。
	defaultExternalPriceGuidance = "当前最新报价：\n{price_list}\n如果需要，请先拍下但不要付款。系统会按您下单时的最新价格自动改价，确认金额后再付款。"
	// defaultExternalPriceAdjustedNotice 是订单按实时货源成本改价成功后的默认付款提示。
	defaultExternalPriceAdjustedNotice = "已按最新价格为您改价为 ¥{price}，请核对订单金额，确认无误后再付款。"
)

// externalPriceMessageConfig 保存付款规则级咨询引导和改价成功通知；金额参数仍由各货源动作独立配置。
type externalPriceMessageConfig struct {
	// GuidanceEnabled 表示买家首次咨询该商品时是否实时查询并发送报价。
	GuidanceEnabled bool `json:"price_guidance_enabled"`
	// QueryPromptText 是查询货源前先发送给买家的等待提示。
	QueryPromptText string `json:"price_query_prompt_text"`
	// GuidanceText 是查询成功后的报价和下单引导文案。
	GuidanceText string `json:"price_guidance_text"`
	// AdjustedNoticeEnabled 表示实时改价成功后是否发送最终价格。
	AdjustedNoticeEnabled bool `json:"price_adjusted_notice_enabled"`
	// AdjustedNoticeText 是改价成功文案，支持最终订单价格等占位符。
	AdjustedNoticeText string `json:"price_adjusted_notice_text"`
}

// parseExternalPriceMessageConfig 解析规则扩展配置并为已开启但留空的文案应用安全默认值。
func parseExternalPriceMessageConfig(raw string) (externalPriceMessageConfig, error) {
	// config 是规则级消息配置；旧规则保持两个开关关闭。
	var config externalPriceMessageConfig
	if strings.TrimSpace(raw) == "" {
		return config, nil
	}
	if unmarshalErr := json.Unmarshal([]byte(raw), &config); unmarshalErr != nil { // unmarshalErr 是规则配置 JSON 解析错误。
		return config, errors.New("实时跟价消息配置不是有效 JSON")
	}
	if config.GuidanceEnabled && (strings.TrimSpace(config.GuidanceText) == "" || strings.TrimSpace(config.GuidanceText) == legacyExternalPriceGuidance) {
		config.GuidanceText = defaultExternalPriceGuidance
	}
	if config.GuidanceEnabled && strings.TrimSpace(config.QueryPromptText) == "" {
		config.QueryPromptText = defaultExternalPriceQueryPrompt
	}
	if config.AdjustedNoticeEnabled && strings.TrimSpace(config.AdjustedNoticeText) == "" {
		config.AdjustedNoticeText = defaultExternalPriceAdjustedNotice
	}
	return config, nil
}

// HandleExternalPriceGuidanceChat 在买家首次咨询已开启跟价的商品时查询货源并发送一次报价；handled 为真时阻止普通回复重复响应。
func (c *Center) HandleExternalPriceGuidanceChat(ctx context.Context, message RechargeChatMessage, itemID string) (handled bool, resultErr error) {
	if c == nil || c.store == nil || strings.TrimSpace(message.AccountID) == "" || strings.TrimSpace(message.ChatID) == "" || strings.TrimSpace(itemID) == "" {
		return false, nil
	}
	// rules 是该商品最高优先级的付款后自动发货规则。
	rules, matchErr := c.store.Automation.Match(ctx, message.AccountID, itemID, TriggerOrderPaid)
	if matchErr != nil || len(rules) == 0 {
		return false, matchErr
	}
	// rule 是唯一生效的商品规则；账号级规则不发送商品咨询引导，避免误伤其他商品。
	rule := rules[0]
	if strings.TrimSpace(rule.ItemID) == "" {
		return false, nil
	}
	// config 和 configErr 是规则级消息开关、文案及解析错误。
	config, configErr := parseExternalPriceMessageConfig(rule.ConfigJSON)
	if configErr != nil || !config.GuidanceEnabled {
		return false, configErr
	}
	// dynamicEnabled 和 dynamicErr 防止数据库历史数据绕过保存校验后给非跟价商品发送错误引导。
	dynamicEnabled, dynamicErr := ruleUsesExternalPendingPrice(rule)
	if dynamicErr != nil || !dynamicEnabled {
		return false, dynamicErr
	}
	// dedupeKey 让同一规则和会话跨重启只成功发送一次咨询引导。
	dedupeKey := fmt.Sprintf("external-price-guide:%d:%s", rule.ID, strings.TrimSpace(message.ChatID))
	// claimed 和 claimErr 表示当前处理者是否取得消息发送租约。
	claimed, claimErr := c.store.Automation.ClaimExternalPriceMessage(ctx, db.ExternalPriceMessageRecord{
		DedupeKey: dedupeKey, CookieID: message.AccountID, ChatID: message.ChatID, ItemID: itemID, RuleID: rule.ID, MessageKind: "guidance",
	})
	if claimErr != nil || !claimed {
		return false, claimErr
	}
	// task 提供在线发送所需的账号、会话、买家和商品事实。
	task := Task{AccountID: message.AccountID, ChatID: message.ChatID, BuyerID: message.BuyerID, ItemID: itemID}
	// promptText 是告诉买家系统正在查询的短提示，不暴露货源凭证。
	promptText := renderExternalPriceMessage(config.QueryPromptText, task, "", "", rule.ItemTitle)
	if strings.TrimSpace(promptText) != "" {
		if sendErr := c.actions.sendText(ctx, task, promptText); sendErr != nil { // sendErr 是查询提示发送失败或结果不确定原因。
			finishErr := c.store.Automation.FinishExternalPriceMessage(ctx, dedupeKey, "failed", sendErr.Error()) // finishErr 是失败状态落库错误。
			return true, errors.Join(sendErr, finishErr)
		}
	}
	// price、priceList、quoteErr 是当前货源实时单规格价格、面向买家的规格价格列表及查询失败原因。
	price, priceList, quoteErr := c.quoteExternalPriceGuidance(ctx, message.AccountID, rule)
	if quoteErr != nil {
		finishErr := c.store.Automation.FinishExternalPriceMessage(ctx, dedupeKey, "failed", quoteErr.Error()) // finishErr 是报价失败状态落库错误。
		return true, errors.Join(quoteErr, finishErr)
	}
	// text 是完成实时价格、规格列表和商品变量替换后的报价下单引导。
	text := renderExternalPriceMessage(config.GuidanceText, task, price, priceList, rule.ItemTitle)
	if sendErr := c.actions.sendText(ctx, task, text); sendErr != nil { // sendErr 是报价引导发送失败或结果不确定原因。
		finishErr := c.store.Automation.FinishExternalPriceMessage(ctx, dedupeKey, "failed", sendErr.Error()) // finishErr 是失败状态落库错误。
		return true, errors.Join(sendErr, finishErr)
	}
	if finishErr := c.store.Automation.FinishExternalPriceMessage(ctx, dedupeKey, "sent", ""); finishErr != nil { // finishErr 是发送成功后的状态持久化错误。
		return true, uncertainAction(fmt.Errorf("咨询引导已发送但状态保存失败: %w", finishErr))
	}
	return true, nil
}

// externalPriceGuidanceQuote 是咨询阶段按闲鱼规格聚合后的单件售价。
type externalPriceGuidanceQuote struct {
	// key 是规格分组的稳定内部标识。
	key string
	// label 是展示给买家的规格或商品名称。
	label string
	// cents 是购买一件闲鱼商品时该规格全部发货内容的合计售价。
	cents int64
}

// quoteExternalPriceGuidance 查询规则中全部实时跟价规格，并生成单规格价格和多规格列表。
func (c *Center) quoteExternalPriceGuidance(ctx context.Context, accountID string, rule db.AutomationRule) (string, string, error) {
	if c.dependencies.externalFulfillment == nil {
		return "", "", errors.New("外部货源报价服务未初始化")
	}
	// userID 是闲鱼账号所属用户，用于隔离其货源实例。
	userID, ownerErr := c.store.Cookies.GetOwnerID(ctx, accountID)
	if ownerErr != nil {
		return "", "", fmt.Errorf("读取报价账号归属: %w", ownerErr)
	}
	// quotes 按规则动作顺序保存首次出现的规格报价。
	quotes := make([]externalPriceGuidanceQuote, 0)
	// quoteIndexes 把同一规格的多条发货内容聚合到同一个报价。
	quoteIndexes := make(map[string]int)
	for _, action := range rule.Actions { // action 是当前待查询的外部发货内容。
		if !action.Enabled || action.ActionType != ActionSendCard {
			continue
		}
		// config、configErr 是当前动作的货源、规格与加价配置。
		config, configErr := parseExternalActionConfig(action.ConfigJSON)
		if configErr != nil {
			return "", "", configErr
		}
		if config.SourceType != "external" || !config.PendingPriceEnabled {
			continue
		}
		// product、quoteErr 是供应商当前商品价格和可采购状态。
		product, quoteErr := c.dependencies.externalFulfillment.QuoteProduct(ctx, userID, config.InstanceID, config.GoodsID)
		if quoteErr != nil {
			return "", "", fmt.Errorf("查询货源商品 %d 实时价格: %w", config.GoodsID, quoteErr)
		}
		if !product.CanBuy {
			return "", "", fmt.Errorf("货源商品 %d 当前不可采购", config.GoodsID)
		}
		// unitCostCents、costErr 是货源单价的整数分结果和格式错误。
		unitCostCents, costErr := parseYuanToCents(product.Price)
		if costErr != nil {
			return "", "", fmt.Errorf("货源商品 %d 没有有效实时价格: %w", config.GoodsID, costErr)
		}
		// fixedMarkupCents、markupErr 是管理员配置的每个采购单位固定加价。
		fixedMarkupCents, markupErr := parseYuanToCents(config.FixedMarkup)
		if markupErr != nil {
			return "", "", fmt.Errorf("货源商品 %d 固定加价无效: %w", config.GoodsID, markupErr)
		}
		// deliveryCount 是买家购买一件闲鱼商品时该动作实际采购的数量。
		deliveryCount := action.DeliveryCount
		if deliveryCount <= 0 {
			deliveryCount = 1
		}
		// actionCents 是该动作对当前规格单件报价的贡献。
		unitTargetCents := unitCostCents + fixedMarkupCents
		if unitTargetCents <= 0 || int64(deliveryCount) > 100000000/unitTargetCents {
			return "", "", fmt.Errorf("货源商品 %d 报价超过闲鱼改价上限", config.GoodsID)
		}
		// actionCents 是当前发货动作对该规格单件售价的贡献。
		actionCents := unitTargetCents * int64(deliveryCount)
		// specKey 和 specLabel 分别用于稳定聚合和买家可读展示。
		specKey, specLabel := externalPriceGuidanceSpec(config)
		// quoteIndex 和 exists 表示同一规格是否已经有待累加的报价。
		quoteIndex, exists := quoteIndexes[specKey]
		if exists {
			if quotes[quoteIndex].cents > 100000000-actionCents {
				return "", "", errors.New("外部货源合计报价超过闲鱼改价上限")
			}
			quotes[quoteIndex].cents += actionCents
			continue
		}
		quoteIndexes[specKey] = len(quotes)
		quotes = append(quotes, externalPriceGuidanceQuote{key: specKey, label: specLabel, cents: actionCents})
	}
	if len(quotes) == 0 {
		return "", "", errors.New("当前规则没有可查询的外部货源报价")
	}
	// price 是单规格规则可直接使用的价格；多规格时留空，避免误导商家模板。
	price := ""
	if len(quotes) == 1 {
		price = formatCentsAsYuan(quotes[0].cents)
	}
	// lines 是按规格逐行展示的当前报价。
	lines := make([]string, 0, len(quotes))
	for _, quote := range quotes { // quote 是当前规格的合计报价。
		if len(quotes) == 1 {
			lines = append(lines, "¥"+formatCentsAsYuan(quote.cents))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s：¥%s", quote.label, formatCentsAsYuan(quote.cents)))
	}
	return price, strings.Join(lines, "\n"), nil
}

// externalPriceGuidanceSpec 返回动作的规格聚合键和买家可读标签。
func externalPriceGuidanceSpec(config externalActionConfig) (string, string) {
	// specName 是规则保存的闲鱼规格名称。
	specName := strings.TrimSpace(config.SpecName)
	// specValue 是优先展示给买家的闲鱼规格值。
	specValue := strings.TrimSpace(config.SpecValue)
	// goodsName 是没有规格值时使用的货源商品摘要。
	goodsName := strings.TrimSpace(config.GoodsName)
	if specValue != "" {
		return specName + "\x00" + specValue, specValue
	}
	if goodsName != "" {
		return "default", goodsName
	}
	return "default", "默认规格"
}

// ruleUsesExternalPendingPrice 判断付款规则是否至少包含一个已启用的外部货源实时跟价动作。
func ruleUsesExternalPendingPrice(rule db.AutomationRule) (bool, error) {
	for _, action := range rule.Actions { // action 是当前待检查的规则动作。
		if !action.Enabled || action.ActionType != ActionSendCard {
			continue
		}
		// config 和 configErr 是外部动作配置及其解析错误。
		config, configErr := parseExternalActionConfig(action.ConfigJSON)
		if configErr != nil {
			return false, configErr
		}
		if config.SourceType == "external" && config.PendingPriceEnabled {
			return true, nil
		}
	}
	return false, nil
}

// sendExternalPriceAdjustedNotice 在真实改价成功后发送最终订单价；独立防重记录避免重复系统卡片重复通知。
func (c *Center) sendExternalPriceAdjustedNotice(ctx context.Context, task Task, rule db.AutomationRule, targetCents int64) error {
	// config 和 configErr 是规则级改价通知配置及解析错误。
	config, configErr := parseExternalPriceMessageConfig(rule.ConfigJSON)
	if configErr != nil || !config.AdjustedNoticeEnabled || strings.TrimSpace(task.ChatID) == "" || strings.TrimSpace(task.BuyerID) == "" {
		return configErr
	}
	// dedupeKey 以订单号为边界，保证每笔成功改价最多确认发送一次。
	dedupeKey := fmt.Sprintf("external-price-adjusted:%s", strings.TrimSpace(task.OrderID))
	// claimed 和 claimErr 表示当前事件是否取得改价通知的发送租约。
	claimed, claimErr := c.store.Automation.ClaimExternalPriceMessage(ctx, db.ExternalPriceMessageRecord{
		DedupeKey: dedupeKey, CookieID: task.AccountID, ChatID: task.ChatID, ItemID: task.ItemID, RuleID: rule.ID,
		OrderID: task.OrderID, MessageKind: "adjusted_notice",
	})
	if claimErr != nil || !claimed {
		return claimErr
	}
	// price 是本笔订单已经成功写入闲鱼的最终总价，两位小数元字符串。
	price := formatCentsAsYuan(targetCents)
	// text 是完成商品、订单数量和最终价格变量替换后的买家通知。
	text := renderExternalPriceMessage(config.AdjustedNoticeText, task, price, price, rule.ItemTitle)
	if sendErr := c.actions.sendText(ctx, task, text); sendErr != nil { // sendErr 是改价通知发送失败或结果不确定原因。
		finishErr := c.store.Automation.FinishExternalPriceMessage(ctx, dedupeKey, "failed", sendErr.Error()) // finishErr 是失败终态保存错误。
		return errors.Join(sendErr, finishErr)
	}
	if finishErr := c.store.Automation.FinishExternalPriceMessage(ctx, dedupeKey, "sent", ""); finishErr != nil { // finishErr 是成功发送后的状态保存错误。
		return uncertainAction(fmt.Errorf("改价通知已发送但状态保存失败: %w", finishErr))
	}
	return nil
}

// renderExternalPriceMessage 替换跟价消息允许使用的非敏感订单变量，未知占位符原样保留。
func renderExternalPriceMessage(template string, task Task, price, priceList, itemTitle string) string {
	// quantity 是订单数量缺失时用于咨询场景展示的空值。
	quantity := strings.TrimSpace(task.Quantity)
	// replacements 是允许商家在话术中引用的非敏感变量和值。
	replacements := map[string]string{
		"{price}": price, "{price_list}": priceList, "{order_price}": price, "{item_id}": task.ItemID, "{item_title}": itemTitle,
		"{quantity}": quantity, "{order_id}": task.OrderID,
	}
	// rendered 是逐个替换允许变量后的最终文本。
	rendered := template
	for token, value := range replacements { // token 和 value 是当前允许占位符及其运行时值。
		rendered = strings.ReplaceAll(rendered, token, value)
	}
	return strings.TrimSpace(rendered)
}
