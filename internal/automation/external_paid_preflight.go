package automation

import (
	"context"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

// preflightExternalDirectPayment 在买家直接付款且没有成功改价快照时，先查实时成本并核对订单级最低利润。
// 全部匹配动作校验通过前不调用 Fulfill，避免多条发货内容出现部分采购。
func (c *Center) preflightExternalDirectPayment(ctx context.Context, task Task, actions []db.AutomationAction) error {
	if task.TriggerType != TriggerOrderPaid || strings.TrimSpace(task.OrderID) == "" {
		return nil
	}
	// externalActions 和 configs 只收集当前规格动作计划中开启待付款跟价的外部货源。
	externalActions := make([]db.AutomationAction, 0, len(actions))
	// configs 与 externalActions 按下标对应，供一次性订单成本核算使用。
	configs := make([]externalActionConfig, 0, len(actions))
	// action 是冻结动作计划中的当前发货动作。
	for _, action := range actions {
		if !action.Enabled || action.ActionType != ActionSendCard {
			continue
		}
		// config 和 configErr 是当前动作的外部货源跟价配置及解析错误。
		config, configErr := parseExternalActionConfig(action.ConfigJSON)
		if configErr != nil {
			return externalFulfillmentFailed(fmt.Errorf("%w: 解析直接付款货源配置: %v", errActionNotPerformed, configErr))
		}
		if config.SourceType == "external" && config.PendingPriceEnabled {
			externalActions = append(externalActions, action)
			configs = append(configs, config)
		}
	}
	if len(externalActions) == 0 {
		return nil
	}

	// adjustedCount 用于判断该订单是否已有明确改价成功的动态保护价。
	adjustedCount := 0
	// action 是需要检查订单级改价快照的当前外部动作。
	for _, action := range externalActions {
		// adjusted 和 quoteErr 表示当前动作是否已有生效动态保护价及读取错误。
		_, adjusted, quoteErr := c.store.Automation.AdjustedExternalSafePrice(ctx, task.OrderID, action.ID)
		if quoteErr != nil {
			return externalFulfillmentFailed(fmt.Errorf("%w: 读取订单改价快照: %v", errActionNotPerformed, quoteErr))
		}
		if adjusted {
			adjustedCount++
		}
	}
	if adjustedCount == len(externalActions) {
		return nil
	}
	if adjustedCount != 0 {
		return externalFulfillmentFailed(fmt.Errorf("%w: 订单外部货源改价快照不完整，已停止采购", errActionNotPerformed))
	}

	// userID 和 ownerErr 是当前闲鱼账号的货源数据隔离用户及读取错误。
	userID, ownerErr := c.store.Cookies.GetOwnerID(ctx, task.AccountID)
	if ownerErr != nil {
		return externalFulfillmentFailed(fmt.Errorf("%w: 读取直接付款账号归属: %v", errActionNotPerformed, ownerErr))
	}
	// 已有任意稳定外部单号时说明采购流程已开始；后续恢复只能查原单，不能重新报价拦截。
	// action 是需要判断稳定外部单号是否已存在的当前动作。
	for _, action := range externalActions {
		// externalOrderNo 是由闲鱼订单和动作生成的稳定幂等单号。
		externalOrderNo := fmt.Sprintf("xy-%s-a%d", strings.TrimSpace(task.OrderID), action.ID)
		// exists 和 existsErr 表示原外部单是否已经创建及查询错误。
		exists, existsErr := c.store.Automation.ExternalFulfillmentOrderExists(ctx, userID, externalOrderNo)
		if existsErr != nil {
			return externalFulfillmentFailed(fmt.Errorf("%w: 检查已有外部货源订单: %v", errActionNotPerformed, existsErr))
		}
		if exists {
			return nil
		}
	}
	// paidCents 是闲鱼订单详情中的买家实付总额。
	paidCents, paidErr := parseYuanToCents(task.Amount)
	if paidErr != nil {
		return externalFulfillmentFailed(fmt.Errorf("%w: 直接付款订单缺少有效实付金额: %v", errActionNotPerformed, paidErr))
	}
	if c.dependencies.externalFulfillment == nil {
		return externalFulfillmentFailed(fmt.Errorf("%w: 外部货源利润预检服务未初始化", errActionNotPerformed))
	}
	// quotes 和 quoteErr 是全部匹配动作的实时成本快照及查价错误。
	quotes, _, quoteErr := c.quotePendingPriceActions(ctx, task, userID, externalActions, configs)
	if quoteErr != nil {
		return externalFulfillmentFailed(fmt.Errorf("%w: 直接付款前查询实时货源成本: %v", errActionNotPerformed, quoteErr))
	}

	// requiredCents 是“全部实时采购成本 + 全部最低利润”，必须不高于买家实付总额。
	var requiredCents int64
	// quote 是当前外部动作的单价、数量和最低利润快照。
	for _, quote := range quotes {
		requiredCents += (quote.unitCostCents + quote.minimumProfitCents) * int64(quote.fulfillmentQuantity)
	}
	if requiredCents > paidCents {
		return externalFulfillmentProfitBlocked(fmt.Errorf("%w: 直接付款订单实付金额不足以覆盖实时采购成本和最低利润", ErrExternalSafePriceExceeded))
	}
	return nil
}
