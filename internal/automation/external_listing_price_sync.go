package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// missingExternalProductListingPriceCents 是货源商品删除后用于提醒人工核对的闲鱼商品售价，单位为分。
const missingExternalProductListingPriceCents int64 = 999900

// scanExternalListingPrices 每轮检查全部开启同步的普通商品，仅在计算售价发生变化时调用闲鱼改价接口。
func (s *Scheduler) scanExternalListingPrices(ctx context.Context) {
	if s == nil || s.center == nil || s.center.dependencies.externalFulfillment == nil {
		return
	}
	// rules、listErr 是按优先级排序的启用付款规则和数据库读取错误。
	rules, listErr := s.center.store.Automation.ListEnabledPaidItemRules(ctx)
	if listErr != nil {
		s.center.logger.Warn("扫描货源价格同步规则失败", "err", listErr)
		return
	}
	// rules 已按账号、商品、优先级排序；同一商品只让优先级最高的规则控制售价。
	processedItems := map[string]struct{}{}
	// quoteInterval 是相邻后台报价的最小起始间隔，零值调度器也使用生产保护值。
	quoteInterval := s.externalListingQuoteInterval
	if quoteInterval <= 0 {
		quoteInterval = defaultExternalListingQuoteInterval
	}
	// lastQuoteStarted 保存本轮最近一次真正发起货源报价的单调时钟时间。
	var lastQuoteStarted time.Time
	// beforeQuote 只在即将访问供应站时报价节流，配置无效或未启用同步的规则不会占用间隔。
	beforeQuote := func(waitCtx context.Context) error { // waitCtx 由调度器 Run 的生命周期 Context 派生并负责取消等待。
		if !lastQuoteStarted.IsZero() {
			// remaining 是距离允许发起下一次报价还需等待的时长。
			remaining := quoteInterval - time.Since(lastQuoteStarted)
			if remaining > 0 {
				// timer 只归当前串行扫描所有，退出前始终停止并释放。
				timer := time.NewTimer(remaining)
				defer timer.Stop()
				select {
				case <-waitCtx.Done():
					return waitCtx.Err()
				case <-timer.C:
				}
			}
		}
		lastQuoteStarted = time.Now()
		return nil
	}
	for _, rule := range rules { // rule 是当前检查商品页价格同步配置的付款规则。
		if ctx.Err() != nil {
			return
		}
		// itemKey 是账号和闲鱼商品组成的本轮去重键。
		itemKey := rule.CookieID + "\x00" + rule.ItemID
		if _, alreadyProcessed := processedItems[itemKey]; alreadyProcessed { // alreadyProcessed 表示更高优先级规则已经控制该商品售价。
			continue
		}
		processedItems[itemKey] = struct{}{}
		// targetCents、enabled、quoteErr 是目标售价分值、同步开关命中状态和实时报价错误。
		targetCents, enabled, quoteErr := s.center.externalListingTargetCentsBeforeQuote(ctx, rule, beforeQuote)
		// productMissing 表示供应站已经明确确认关联商品被删除。
		productMissing := errors.Is(quoteErr, ErrExternalProductNotFound)
		if quoteErr != nil {
			if !productMissing {
				s.center.logger.Warn("查询商品自动同步价格失败", "account", rule.CookieID, "item_id", rule.ItemID, "rule_id", rule.ID, "err", quoteErr)
				continue
			}
			targetCents = missingExternalProductListingPriceCents
			enabled = true
		}
		if !enabled {
			continue
		}
		// item、itemErr 是当前闲鱼商品快照和数据库读取错误。
		item, itemErr := s.center.store.Items.GetByCookieItem(ctx, rule.CookieID, rule.ItemID)
		if itemErr != nil {
			s.center.logger.Warn("读取价格同步商品失败", "account", rule.CookieID, "item_id", rule.ItemID, "err", itemErr)
			continue
		}
		if item.IsMultiSpec {
			s.center.logger.Debug("多规格商品跳过商品页自动改价，保留订单级跟价", "account", rule.CookieID, "item_id", rule.ItemID)
			continue
		}
		// currentCents、currentErr 是本地商品现价分值和金额解析错误。
		currentCents, currentErr := parseYuanToCents(strings.TrimSpace(strings.TrimPrefix(item.ItemPrice, "¥")))
		if currentErr == nil && currentCents == targetCents {
			continue
		}
		// allowed、allowErr 是账号能否执行自动化外部动作的状态和检查错误。
		allowed, allowErr := s.center.accountAutomationAllowed(ctx, rule.CookieID)
		if allowErr != nil || !allowed {
			continue
		}
		if syncErr := s.center.actions.syncItemListingPrice(ctx, rule.CookieID, rule.ItemID, targetCents, true); syncErr != nil { // syncErr 是闲鱼商品远程改价错误。
			s.center.logger.Warn("同步闲鱼商品售价失败", "account", rule.CookieID, "item_id", rule.ItemID, "target_price", formatCentsAsYuan(targetCents), "err", syncErr)
			continue
		}
		item.ItemPrice = formatCentsAsYuan(targetCents)
		if saveErr := s.center.store.Items.Upsert(ctx, &item); saveErr != nil { // saveErr 是远程改价成功后的本地价格保存错误。
			s.center.logger.Warn("闲鱼商品已改价但本地价格保存失败", "account", rule.CookieID, "item_id", rule.ItemID, "target_price", item.ItemPrice, "err", saveErr)
			continue
		}
		if productMissing {
			s.center.logger.Warn("货源商品不存在，已将闲鱼商品调整为人工核对价", "account", rule.CookieID, "item_id", rule.ItemID, "rule_id", rule.ID, "target_price", item.ItemPrice)
		} else {
			s.center.logger.Info("已按货源采购价和利润率同步闲鱼商品售价", "account", rule.CookieID, "item_id", rule.ItemID, "target_price", item.ItemPrice)
		}
	}
}

// externalListingTargetCents 计算普通商品单件售价；带规格映射的规则只保留订单级自动改价。
func (c *Center) externalListingTargetCents(ctx context.Context, rule db.AutomationRule) (int64, bool, error) {
	return c.externalListingTargetCentsBeforeQuote(ctx, rule, nil)
}

// externalListingTargetCentsBeforeQuote 在每次实际货源报价前调用钩子，供后台调度限速且不影响主动询价。
func (c *Center) externalListingTargetCentsBeforeQuote(ctx context.Context, rule db.AutomationRule, beforeQuote func(context.Context) error) (int64, bool, error) {
	// targetCents 累加同一规则所有外部发货动作对应的闲鱼目标售价，单位为分。
	var targetCents int64
	// enabled 表示规则中至少存在一个有效的外部商品页价格同步动作。
	enabled := false
	for _, action := range rule.Actions { // action 是当前待检查的自动化发货动作。
		if !action.Enabled || action.ActionType != ActionSendCard {
			continue
		}
		// config、configErr 是外部货源动作配置和持久化 JSON 解析错误。
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
		if beforeQuote != nil {
			if waitErr := beforeQuote(ctx); waitErr != nil { // waitErr 是后台报价限速被服务关闭取消后的结果。
				return 0, true, waitErr
			}
		}
		// product、quoteErr 是供应站实时商品报价和查询错误。
		product, quoteErr := c.dependencies.externalFulfillment.QuoteProduct(ctx, rule.UserID, config.InstanceID, config.GoodsID)
		if quoteErr != nil {
			return 0, true, fmt.Errorf("查询货源商品 %d 实时价格: %w", config.GoodsID, quoteErr)
		}
		if !product.CanBuy {
			return 0, true, fmt.Errorf("货源商品 %d 当前不可采购", config.GoodsID)
		}
		// costCents、costErr 是实时采购单价分值和金额解析错误。
		costCents, costErr := parseYuanToCents(product.Price)
		if costErr != nil {
			return 0, true, fmt.Errorf("货源商品 %d 采购价无效: %w", config.GoodsID, costErr)
		}
		// unitTarget、pricingErr 是单个发货单位的目标售价分值和利润配置错误。
		unitTarget, _, _, pricingErr := externalUnitTargetCents(config, costCents)
		if pricingErr != nil {
			return 0, true, pricingErr
		}
		// count 是每件闲鱼商品需要采购的远程商品份数，旧规则缺失时按一份处理。
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
