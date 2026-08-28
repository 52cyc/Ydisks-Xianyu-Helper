package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

// scanExternalListingPrices 每轮检查全部开启同步的普通商品，仅在计算售价发生变化时调用闲鱼改价接口。
func (s *Scheduler) scanExternalListingPrices(ctx context.Context) {
	if s == nil || s.center == nil || s.center.dependencies.externalFulfillment == nil {
		return
	}
	rules, listErr := s.center.store.Automation.ListEnabledPaidItemRules(ctx)
	if listErr != nil {
		s.center.logger.Warn("扫描货源价格同步规则失败", "err", listErr)
		return
	}
	// rules 已按账号、商品、优先级排序；同一商品只让优先级最高的规则控制售价。
	processedItems := map[string]struct{}{}
	for _, rule := range rules {
		if ctx.Err() != nil {
			return
		}
		itemKey := rule.CookieID + "\x00" + rule.ItemID
		if _, alreadyProcessed := processedItems[itemKey]; alreadyProcessed {
			continue
		}
		processedItems[itemKey] = struct{}{}
		targetCents, enabled, quoteErr := s.center.externalListingTargetCents(ctx, rule)
		if quoteErr != nil {
			s.center.logger.Warn("查询商品自动同步价格失败", "account", rule.CookieID, "item_id", rule.ItemID, "rule_id", rule.ID, "err", quoteErr)
			continue
		}
		if !enabled {
			continue
		}
		item, itemErr := s.center.store.Items.GetByCookieItem(ctx, rule.CookieID, rule.ItemID)
		if itemErr != nil {
			s.center.logger.Warn("读取价格同步商品失败", "account", rule.CookieID, "item_id", rule.ItemID, "err", itemErr)
			continue
		}
		if item.IsMultiSpec {
			s.center.logger.Debug("多规格商品跳过商品页自动改价，保留订单级跟价", "account", rule.CookieID, "item_id", rule.ItemID)
			continue
		}
		currentCents, currentErr := parseYuanToCents(strings.TrimSpace(strings.TrimPrefix(item.ItemPrice, "¥")))
		if currentErr == nil && currentCents == targetCents {
			continue
		}
		allowed, allowErr := s.center.accountAutomationAllowed(ctx, rule.CookieID)
		if allowErr != nil || !allowed {
			continue
		}
		if syncErr := s.center.actions.syncItemListingPrice(ctx, rule.CookieID, rule.ItemID, targetCents, true); syncErr != nil {
			s.center.logger.Warn("同步闲鱼商品售价失败", "account", rule.CookieID, "item_id", rule.ItemID, "target_price", formatCentsAsYuan(targetCents), "err", syncErr)
			continue
		}
		item.ItemPrice = formatCentsAsYuan(targetCents)
		if saveErr := s.center.store.Items.Upsert(ctx, &item); saveErr != nil {
			s.center.logger.Warn("闲鱼商品已改价但本地价格保存失败", "account", rule.CookieID, "item_id", rule.ItemID, "target_price", item.ItemPrice, "err", saveErr)
			continue
		}
		s.center.logger.Info("已按货源采购价和利润率同步闲鱼商品售价", "account", rule.CookieID, "item_id", rule.ItemID, "target_price", item.ItemPrice)
	}
}

// externalListingTargetCents 计算普通商品单件售价；带规格映射的规则只保留订单级自动改价。
func (c *Center) externalListingTargetCents(ctx context.Context, rule db.AutomationRule) (int64, bool, error) {
	var targetCents int64
	enabled := false
	for _, action := range rule.Actions {
		if !action.Enabled || action.ActionType != ActionSendCard {
			continue
		}
		config, configErr := parseExternalActionConfig(action.ConfigJSON)
		if configErr != nil {
			return 0, true, configErr
		}
		if config.SourceType != "external" || !config.PriceSyncEnabled {
			continue
		}
		enabled = true
		if strings.TrimSpace(config.SpecName) != "" || strings.TrimSpace(config.SpecValue) != "" {
			return 0, true, errors.New("多规格价格同步仅支持订单级改价")
		}
		product, quoteErr := c.dependencies.externalFulfillment.QuoteProduct(ctx, rule.UserID, config.InstanceID, config.GoodsID)
		if quoteErr != nil {
			return 0, true, fmt.Errorf("查询货源商品 %d 实时价格: %w", config.GoodsID, quoteErr)
		}
		if !product.CanBuy {
			return 0, true, fmt.Errorf("货源商品 %d 当前不可采购", config.GoodsID)
		}
		costCents, costErr := parseYuanToCents(product.Price)
		if costErr != nil {
			return 0, true, fmt.Errorf("货源商品 %d 采购价无效: %w", config.GoodsID, costErr)
		}
		unitTarget, _, _, pricingErr := externalUnitTargetCents(config, costCents)
		if pricingErr != nil {
			return 0, true, pricingErr
		}
		count := action.DeliveryCount
		if count <= 0 {
			count = 1
		}
		if int64(count) > 100000000/unitTarget || targetCents > 100000000-unitTarget*int64(count) {
			return 0, true, errors.New("商品同步价格超过闲鱼金额上限")
		}
		targetCents += unitTarget * int64(count)
	}
	if !enabled {
		return 0, false, nil
	}
	if targetCents <= 0 {
		return 0, true, errors.New("商品同步价格无效")
	}
	return targetCents, true, nil
}
