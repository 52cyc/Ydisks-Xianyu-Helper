package automation

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"xianyu-go/internal/db"
)

// pendingPriceActionQuote 保存一次实时查询后可用于计算闲鱼售价和动作保护价的内存快照。
type pendingPriceActionQuote struct {
	// action 是付款后真正执行采购的外部货源动作。
	action db.AutomationAction
	// config 是动作中已经通过保存校验的外部货源与跟价参数。
	config externalActionConfig
	// unitCostCents 是货源实时单价，单位为分。
	unitCostCents int64
	// fulfillmentQuantity 是结合订单数量和每件份数后的采购数量。
	fulfillmentQuantity int
	// fixedMarkupCents 是每个采购单位的固定加价，单位为分。
	fixedMarkupCents int64
	// minimumProfitCents 是付款时每个采购单位必须保留的利润，单位为分。
	minimumProfitCents int64
	// unitTargetCents 是按利润率计算后的每个采购单位售价；旧规则仍按固定加价计算。
	unitTargetCents int64
}

// handleOrderCreatedPricing 按互斥顺序让 AI 报价或外部货源实时价接管待付款订单；prepared 保留后续固定规则需要的任务事实。
func (c *Center) handleOrderCreatedPricing(ctx context.Context, task Task) (handled bool, prepared Task, err error) {
	// aiPricingActive、aiPricingErr 是 AI 议价模式的接管状态和执行错误。
	aiPricingActive, aiPricingErr := c.handleAIPricingMode(ctx, task)
	if aiPricingActive || aiPricingErr != nil {
		return aiPricingActive, task, aiPricingErr
	}
	return c.handleExternalPendingPrice(ctx, task)
}

// handleExternalPendingPrice 在未付款事件中复用付款后发货规则，按匹配规格的全部外部动作实时成本修改闲鱼订单总价。
// handled 表示已发现动态跟价配置或历史报价，调用方不得再执行独立固定改价规则；prepared 返回补齐后的订单事实。
func (c *Center) handleExternalPendingPrice(ctx context.Context, task Task) (handled bool, prepared Task, err error) {
	prepared = task
	if task.TriggerType != TriggerOrderCreated || strings.TrimSpace(task.OrderID) == "" {
		return false, prepared, nil
	}
	// exists、quoteStatus、stateErr 用于识别重复系统卡片和上一次结果未知的改价，任何已有报价都禁止再次请求平台。
	exists, quoteStatus, stateErr := c.store.Automation.ExternalPriceQuoteState(ctx, task.OrderID)
	if stateErr != nil {
		return true, prepared, fmt.Errorf("读取订单跟价状态: %w", stateErr)
	}
	if exists {
		c.logger.Info("订单已有货源跟价记录，忽略重复待付款事件", "order_id", task.OrderID, "status", quoteStatus)
		if quoteStatus == "adjusted" {
			// paidRule 和 configuredDynamic 用于让改价已成功但通知失败的重复事件安全补发一次最终价格。
			paidRule, configuredDynamic, ruleErr := c.externalPendingPriceRule(ctx, task)
			if ruleErr != nil || !configuredDynamic || paidRule == nil {
				return true, prepared, ruleErr
			}
			// targetCents 和 storedStatus 是已落库的最终订单价及终态，避免再次查询货源或修改价格。
			targetCents, storedStatus, targetErr := c.store.Automation.ExternalPriceQuoteTarget(ctx, task.OrderID)
			if targetErr != nil {
				return true, prepared, targetErr
			}
			if storedStatus == "adjusted" {
				return true, prepared, c.sendExternalPriceAdjustedNotice(ctx, prepared, *paidRule, targetCents)
			}
		}
		return true, prepared, nil
	}
	// paidRule、configuredDynamic、ruleErr 是商品付款规则、是否配置任一跟价动作和匹配错误。
	paidRule, configuredDynamic, ruleErr := c.externalPendingPriceRule(ctx, task)
	if ruleErr != nil || !configuredDynamic {
		return configuredDynamic, prepared, ruleErr
	}
	if paidRule == nil {
		return false, prepared, nil
	}
	// pricingTask 明确要求准备阶段补齐订单详情，普通固定改价事件不会承担该查询依赖。
	pricingTask := task
	pricingTask.RequireOrderDetail = true
	// preparedTask 补齐订单真实规格和数量，避免按商品默认规格或数量一计算错误售价。
	preparedTask, prepareErr := c.prepareTask(ctx, pricingTask)
	if prepareErr != nil {
		return true, prepared, fmt.Errorf("读取待付款订单规格和数量: %w", prepareErr)
	}
	prepared = preparedTask
	// matchedActions、configs、specDynamic、actionsErr 是实际规格的发货动作、配置、跟价状态和完整成本校验结果。
	matchedActions, configs, specDynamic, actionsErr := pendingPriceActions(prepared, *paidRule)
	if actionsErr != nil || !specDynamic {
		return specDynamic, prepared, actionsErr
	}
	if len(matchedActions) == 0 {
		return false, prepared, nil
	}
	// userID 是闲鱼账号所属用户，用于隔离货源实例的实时商品查询。
	userID, ownerErr := c.store.Cookies.GetOwnerID(ctx, prepared.AccountID)
	if ownerErr != nil {
		return true, prepared, fmt.Errorf("读取跟价账号归属: %w", ownerErr)
	}
	if c.dependencies.externalFulfillment == nil {
		return true, prepared, errors.New("外部货源跟价服务未初始化")
	}
	// actionQuotes、targetOrderCents、quoteErr 是实时货源快照、闲鱼目标总价和查询或金额错误。
	actionQuotes, targetOrderCents, quoteErr := c.quotePendingPriceActions(ctx, prepared, userID, matchedActions, configs)
	if quoteErr != nil {
		return true, prepared, quoteErr
	}
	return true, prepared, c.persistAndApplyPendingPrice(ctx, prepared, *paidRule, actionQuotes, targetOrderCents)
}

// externalPendingPriceRule 在读取订单详情前查找商品付款规则，并判断是否存在任一启用的外部跟价动作。
func (c *Center) externalPendingPriceRule(ctx context.Context, task Task) (*db.AutomationRule, bool, error) {
	// paidTask 仅用于查询同商品付款规则，不会创建付款运行或执行采购。
	paidTask := task
	paidTask.TriggerType = TriggerOrderPaid
	// paidRules 是当前商品按优先级唯一生效的付款后发货规则。
	paidRules, matchErr := c.rules.match(ctx, paidTask)
	if matchErr != nil || len(paidRules) == 0 {
		return nil, false, matchErr
	}
	// action 是尚未按规格过滤的付款发货动作。
	for _, action := range paidRules[0].Actions {
		if !action.Enabled || action.ActionType != ActionSendCard {
			continue
		}
		// config、configErr 是用于识别跟价开关的动作配置和解析结果。
		config, configErr := parseExternalActionConfig(action.ConfigJSON)
		if configErr != nil {
			return &paidRules[0], true, configErr
		}
		if config.SourceType == "external" && config.priceSyncEnabled() {
			return &paidRules[0], true, nil
		}
	}
	return &paidRules[0], false, nil
}

// pendingPriceActions 筛选订单规格对应的全部发货动作；只要一项开启跟价，其他匹配动作也必须全部开启。
func pendingPriceActions(task Task, rule db.AutomationRule) ([]db.AutomationAction, []externalActionConfig, bool, error) {
	// actions 保存与订单实际规格匹配的发货动作。
	actions := make([]db.AutomationAction, 0, len(rule.Actions))
	// configs 保存与 actions 同序的外部货源配置。
	configs := make([]externalActionConfig, 0, len(rule.Actions))
	// dynamicEnabled 表示当前规格至少一项外部发货内容要求跟价。
	dynamicEnabled := false
	for _, action := range rule.Actions { // action 是当前规则动作。
		if !action.Enabled || action.ActionType != ActionSendCard || !actionMatchesOrderSpec(task, action) {
			continue
		}
		// config、configErr 是当前匹配动作配置和解析错误。
		config, configErr := parseExternalActionConfig(action.ConfigJSON)
		if configErr != nil {
			return nil, nil, true, configErr
		}
		actions, configs = append(actions, action), append(configs, config)
		dynamicEnabled = dynamicEnabled || (config.SourceType == "external" && config.priceSyncEnabled())
	}
	if !dynamicEnabled {
		return actions, configs, false, nil
	}
	for configIndex, config := range configs { // configIndex、config 是当前匹配动作位置和货源配置。
		if config.SourceType != "external" || !config.priceSyncEnabled() {
			return nil, nil, true, fmt.Errorf("外部货源待付款跟价要求同一规格的全部发货内容都开启跟价，第 %d 条未开启", configIndex+1)
		}
	}
	return actions, configs, true, nil
}

// quotePendingPriceActions 查询所有匹配货源的实时单价并计算动作快照和闲鱼订单目标总价。
func (c *Center) quotePendingPriceActions(ctx context.Context, task Task, userID int64, actions []db.AutomationAction, configs []externalActionConfig) ([]pendingPriceActionQuote, int64, error) {
	// actionQuotes 保存全部实时成本；所有查询成功后才会持久化并执行一次订单改价。
	actionQuotes := make([]pendingPriceActionQuote, 0, len(actions))
	// targetOrderCents 是全部外部发货动作成本和固定加价之和，单位为分。
	var targetOrderCents int64
	// actionIndex 是当前匹配动作下标；action 是对应付款后采购动作。
	for actionIndex, action := range actions {
		// config 是与当前动作同序的外部货源跟价配置。
		config := configs[actionIndex]
		// product、quoteErr 是实时商品摘要和货源查询失败原因。
		product, quoteErr := c.dependencies.externalFulfillment.QuoteProduct(ctx, userID, config.InstanceID, config.GoodsID)
		if quoteErr != nil {
			return nil, 0, fmt.Errorf("查询货源商品 %d 实时价格: %w", config.GoodsID, quoteErr)
		}
		if !product.CanBuy {
			return nil, 0, fmt.Errorf("货源商品 %d 当前不可采购，保持闲鱼原价", config.GoodsID)
		}
		// unitCostCents、costErr 是货源十进制单价及格式校验结果。
		unitCostCents, costErr := parseYuanToCents(product.Price)
		if costErr != nil {
			return nil, 0, fmt.Errorf("货源商品 %d 没有有效实时价格: %w", config.GoodsID, costErr)
		}
		// unitTargetCents、fixedMarkupCents、minimumProfitCents 同时兼容新利润率模型和旧固定加价模型。
		unitTargetCents, fixedMarkupCents, minimumProfitCents, pricingErr := externalUnitTargetCents(config, unitCostCents)
		if pricingErr != nil {
			return nil, 0, fmt.Errorf("货源商品 %d 售价配置无效: %w", config.GoodsID, pricingErr)
		}
		// fulfillmentQuantity 是当前动作真正采购的单位数。
		fulfillmentQuantity := deliverySendCount(task, action)
		// unitTargetCents 是当前动作每个采购单位对闲鱼订单总价的贡献。
		if fulfillmentQuantity <= 0 || int64(fulfillmentQuantity) > 100000000/unitTargetCents {
			return nil, 0, fmt.Errorf("货源商品 %d 按订单数量计算后超过闲鱼改价上限", config.GoodsID)
		}
		// actionTargetCents 是当前动作按实际采购数量计算的售价贡献。
		actionTargetCents := unitTargetCents * int64(fulfillmentQuantity)
		if targetOrderCents > 100000000-actionTargetCents {
			return nil, 0, errors.New("外部货源合计价格超过闲鱼改价上限")
		}
		targetOrderCents += actionTargetCents
		actionQuotes = append(actionQuotes, pendingPriceActionQuote{action: action, config: config, unitCostCents: unitCostCents,
			fulfillmentQuantity: fulfillmentQuantity, fixedMarkupCents: fixedMarkupCents, minimumProfitCents: minimumProfitCents,
			unitTargetCents: unitTargetCents})
	}
	if targetOrderCents <= 0 {
		return nil, 0, errors.New("外部货源跟价计算出的订单价格无效")
	}
	return actionQuotes, targetOrderCents, nil
}

// persistAndApplyPendingPrice 原子保存订单动作报价、执行一次闲鱼改价并收口报价状态。
func (c *Center) persistAndApplyPendingPrice(ctx context.Context, task Task, rule db.AutomationRule, actionQuotes []pendingPriceActionQuote, targetOrderCents int64) error {
	// storedQuotes 是即将原子保存的动作级报价；动态保护价按动作自己的采购总额和最低利润计算。
	storedQuotes := make([]db.ExternalPriceQuote, 0, len(actionQuotes))
	// actionQuote 是当前已查询成功的动作成本快照。
	for _, actionQuote := range actionQuotes {
		// 新利润率模型以售价为倒挂边界；旧规则继续扣除最低利润，保持升级前采购语义。
		dynamicSafeUnitCents := actionQuote.unitTargetCents
		if strings.TrimSpace(actionQuote.config.ProfitRate) == "" {
			dynamicSafeUnitCents -= actionQuote.minimumProfitCents
		}
		dynamicSafeCents := dynamicSafeUnitCents * int64(actionQuote.fulfillmentQuantity)
		storedQuotes = append(storedQuotes, db.ExternalPriceQuote{OrderID: task.OrderID, CookieID: task.AccountID,
			ActionID: actionQuote.action.ID, UnitCostCents: actionQuote.unitCostCents, FulfillmentQuantity: actionQuote.fulfillmentQuantity,
			FixedMarkupCents: actionQuote.fixedMarkupCents, MinimumProfitCents: actionQuote.minimumProfitCents,
			TargetOrderCents: targetOrderCents, DynamicSafePrice: formatCentsAsYuan(dynamicSafeCents), Status: "pending"})
	}
	// created 表示当前处理者取得该订单的唯一改价权；并发重复事件只读取已有报价。
	created, createErr := c.store.Automation.CreateExternalPriceQuotes(ctx, storedQuotes)
	if createErr != nil {
		return createErr
	}
	if !created {
		return nil
	}
	// adjustErr 是包含平台暂忙重试和凭证恢复语义的一次真实订单改价结果。
	adjustErr := c.actions.adjustOrderPriceWithRetry(ctx, task, targetOrderCents)
	if errors.Is(adjustErr, errAdjustPriceNaturallyClosed) {
		// finishErr 尝试把仍处于 pending 的报价收口；付款流程可能已并发替换为 adjusted 快照。
		finishErr := c.store.Automation.FinishExternalPriceQuotes(ctx, task.OrderID, "failed", adjustErr.Error())
		if finishErr != nil {
			// exists、status、stateErr 用于确认付款流程是否已经合法接管报价，避免把并发终态误报为保存失败。
			exists, status, stateErr := c.store.Automation.ExternalPriceQuoteState(ctx, task.OrderID)
			if stateErr != nil || !exists || status != "adjusted" {
				return errors.Join(finishErr, stateErr)
			}
		}
		c.logger.Info("订单已付款或状态已结束，货源跟价流程自然结束", "account", task.AccountID, "order_id", task.OrderID)
		return nil
	}
	if adjustErr != nil {
		// finishErr 保存失败原因；报价保持 failed 后付款会安全回退固定保护价。
		finishErr := c.store.Automation.FinishExternalPriceQuotes(ctx, task.OrderID, "failed", adjustErr.Error())
		if finishErr != nil {
			return errors.Join(adjustErr, fmt.Errorf("保存跟价失败状态: %w", finishErr))
		}
		return adjustErr
	}
	if finishErr := c.store.Automation.FinishExternalPriceQuotes(ctx, task.OrderID, "adjusted", ""); finishErr != nil { // finishErr 是成功改价后的报价终态保存失败原因。
		return uncertainAction(fmt.Errorf("闲鱼已完成货源跟价，但报价状态保存失败: %w", finishErr))
	}
	c.logger.Info("已按外部货源实时价格修改待付款订单", "account", task.AccountID, "order_id", task.OrderID,
		"target_price", formatCentsAsYuan(targetOrderCents), "actions", len(actionQuotes))
	return c.sendExternalPriceAdjustedNotice(ctx, task, rule, targetOrderCents)
}

// externalUnitTargetCents 按利润率计算售价并向上取整到分；缺少新字段时兼容旧固定加价配置。
func externalUnitTargetCents(config externalActionConfig, unitCostCents int64) (targetCents, markupCents, legacyMinimumProfitCents int64, err error) {
	if strings.TrimSpace(config.ProfitRate) != "" {
		// rateHundredths 是百分比的百分之一，例如 2.50% 保存为 250。
		rateHundredths, rateErr := parseProfitRateHundredths(config.ProfitRate)
		if rateErr != nil {
			return 0, 0, 0, rateErr
		}
		if unitCostCents > 100000000 || unitCostCents > (1<<62)/(10000+rateHundredths) {
			return 0, 0, 0, errors.New("按利润率计算后超过金额上限")
		}
		targetCents = (unitCostCents*(10000+rateHundredths) + 9999) / 10000
		if targetCents <= 0 || targetCents > 100000000 {
			return 0, 0, 0, errors.New("按利润率计算后的售价无效")
		}
		return targetCents, targetCents - unitCostCents, 0, nil
	}
	if strings.TrimSpace(config.FixedMarkup) == "" {
		return unitCostCents, 0, 0, nil
	}
	markupCents, markupErr := parseYuanToCents(config.FixedMarkup)
	if markupErr != nil {
		return 0, 0, 0, fmt.Errorf("固定加价无效: %w", markupErr)
	}
	minimumProfitCents := int64(0)
	var profitErr error
	if strings.TrimSpace(config.MinimumProfit) != "" {
		minimumProfitCents, profitErr = parseNonNegativeYuanToCents(config.MinimumProfit)
	}
	if profitErr != nil {
		return 0, 0, 0, fmt.Errorf("最低保留利润无效: %w", profitErr)
	}
	if minimumProfitCents > markupCents {
		return 0, 0, 0, errors.New("最低保留利润不能大于固定加价")
	}
	return unitCostCents + markupCents, markupCents, minimumProfitCents, nil
}

// parseProfitRateHundredths 解析 0 到 1000%、最多两位小数的利润率。
func parseProfitRateHundredths(raw string) (int64, error) {
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

// parseNonNegativeYuanToCents 解析允许为零的十进制元金额，最多两位小数且不得超过系统金额上限。
func parseNonNegativeYuanToCents(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "0" || raw == "0.0" || raw == "0.00" {
		return 0, nil
	}
	return parseYuanToCents(raw)
}

// formatCentsAsYuan 把整数分格式化为货源和闲鱼均可接受的两位小数元字符串。
func formatCentsAsYuan(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}
