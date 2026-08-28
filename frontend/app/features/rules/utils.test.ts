import { describe,expect,test } from 'vitest';
import type { AccountDetail,ShippingRule } from './api';
import {
accentClasses,
accountLabel,
actionSummary,
adjustPriceTarget,
boolFlag,
buildAdjustPriceConfig,
buildExternalPriceMessageConfig,
buildReviewConfig,
cardActionsForTrigger,
defaultRuleName,
emptyVariant,
fulfillmentGoodsID,
isValidAdjustPrice,
isValidNonNegativeMoney,
isValidProfitRate,
externalSalePrice,
parseJSONObject,
recommendedExternalPricing,
shouldReplaceGeneratedName,
statusPill,
} from './utils';

// rule 是规则工具测试使用的最小规则对象。
const rule = (overrides: Partial<ShippingRule> = {}): ShippingRule => ({
  id: 'rule-1',
  name: '测试规则',
  trigger_type: 'order_paid',
  item_keyword: '',
  card_group_id: 0,
  priority: 1,
  enabled: true,
  actions: [],
  variants: [],
  ...overrides,
});

describe('规则工具函数', /* 当前回调处理规则配置和展示状态。 */ () => {
  test('安全解析规则配置并规范化求评价参数', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(parseJSONObject()).toEqual({});
    expect(parseJSONObject('{bad')).toEqual({});
    expect(parseJSONObject('[]')).toEqual({});
    expect(parseJSONObject('{"max_attempts": 3}')).toEqual({ max_attempts: 3 });
    expect(buildReviewConfig('{"after_shipped_hours": 48}')).toBe('{"after_shipped_hours":48,"repeat_interval_hours":24,"max_attempts":1}');
    expect(buildReviewConfig(undefined, { max_attempts: 4 })).toBe('{"after_shipped_hours":72,"repeat_interval_hours":24,"max_attempts":4}');
  });

  test('合并实时跟价消息配置时保留规则其他字段', /* 当前回调验证聊天话术开关不会覆盖已有规则扩展配置。 */ () => {
    expect(buildExternalPriceMessageConfig('{"existing":1}', {
      price_guidance_enabled: true,
      price_guidance_text: '请先拍下不要付款',
      fulfillment_failure_notice_enabled: true,
      fulfillment_safe_price_notice_text: '最新价格 {price}',
      fulfillment_failure_notice_text: '订单正在人工核实',
    })).toBe('{"existing":1,"price_guidance_enabled":true,"price_guidance_text":"请先拍下不要付款","fulfillment_failure_notice_enabled":true,"fulfillment_safe_price_notice_text":"最新价格 {price}","fulfillment_failure_notice_text":"订单正在人工核实"}');
  });

  test('生成规则名称并识别可替换的系统名称', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(defaultRuleName('order_paid')).toBe('付款后自动发货');
    expect(defaultRuleName('buyer_reviewed', '数字商品')).toBe('评价后发送赠品 - 数字商品');
    expect(defaultRuleName('unknown' as never)).toBe('自动化规则');
    expect(shouldReplaceGeneratedName()).toBe(true);
    expect(shouldReplaceGeneratedName('  ')).toBe(true);
    expect(shouldReplaceGeneratedName('付款后自动发货')).toBe(true);
    expect(shouldReplaceGeneratedName('付款后自动发货 - 旧商品')).toBe(true);
    expect(shouldReplaceGeneratedName('我自定义的规则')).toBe(false);
  });

  test('创建各类触发器的默认动作链', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(cardActionsForTrigger('review_missing_timeout')).toEqual([{
      action_type: 'send_text', message_template: '亲，商品使用满意的话，麻烦给个评价，谢谢～', enabled: true, sort_order: 1,
    }]);
    expect(cardActionsForTrigger('order_created')).toEqual([{
      action_type: 'adjust_price', config_json: '{"target_price":""}', enabled: true, sort_order: 1,
    }]);
    expect(cardActionsForTrigger('order_paid', 7)).toEqual([
      { action_type: 'send_card', card_id: 7, delivery_count: 1, enabled: true, sort_order: 1 },
      { action_type: 'confirm_shipment', enabled: true, sort_order: 2 },
    ]);
    expect(cardActionsForTrigger('buyer_reviewed', 8)).toEqual([
      { action_type: 'send_card', card_id: 8, delivery_count: 1, enabled: true, sort_order: 1 },
    ]);
  });

  test('校验并读取拍下改价的目标价格', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(buildAdjustPriceConfig(' 9.9 ')).toBe('{"target_price":"9.9"}');
    expect(adjustPriceTarget([{ action_type: 'adjust_price', config_json: '{"target_price":"12.5"}', enabled: true }])).toBe('12.5');
    expect(adjustPriceTarget([{ action_type: 'send_text', message_template: 'x', enabled: true }])).toBe('');
    expect(adjustPriceTarget()).toBe('');
    expect(isValidAdjustPrice('9.9')).toBe(true);
    expect(isValidAdjustPrice('0.01')).toBe(true);
    expect(isValidAdjustPrice('1000000')).toBe(true);
    expect(isValidAdjustPrice('0')).toBe(false);
    expect(isValidAdjustPrice('')).toBe(false);
    expect(isValidAdjustPrice('1.234')).toBe(false);
    expect(isValidAdjustPrice('abc')).toBe(false);
    expect(isValidAdjustPrice('1000000.01')).toBe(false);
    expect(actionSummary(rule({ trigger_type: 'order_created', actions: [{ action_type: 'adjust_price', config_json: '{"target_price":"9.9"}', enabled: true }] }))).toBe('拍下后改价为 ¥9.9');
    expect(actionSummary(rule({ trigger_type: 'order_created', actions: [] }))).toBe('未配置目标价格');
  });

  test('校验外部货源固定加价和最低利润金额', /* 当前回调验证跟价金额允许零利润但拒绝负数和多位小数。 */ () => {
    expect(isValidNonNegativeMoney('0')).toBe(true);
    expect(isValidNonNegativeMoney('0.20')).toBe(true);
    expect(isValidNonNegativeMoney('0.5')).toBe(true);
    expect(isValidNonNegativeMoney('-0.1')).toBe(false);
    expect(isValidNonNegativeMoney('0.123')).toBe(false);
    expect(isValidNonNegativeMoney('')).toBe(false);
  });

  test('每次商品校验覆盖简化后的默认价格策略', /* 当前回调验证利润率、自动同步和倒挂保护默认值。 */ () => {
    expect(recommendedExternalPricing('13.43')).toEqual({
      profit_rate: '2.00',
      price_sync_enabled: true,
      pending_price_enabled: false,
      stop_purchase_on_inversion: true,
    });
    expect(recommendedExternalPricing('-')).toEqual({
      profit_rate: '2.00',
      price_sync_enabled: true,
      pending_price_enabled: false,
      stop_purchase_on_inversion: true,
    });
    expect(isValidProfitRate('2.50')).toBe(true);
    expect(isValidProfitRate('1000.01')).toBe(false);
    expect(externalSalePrice('9.50', '2.00')).toBe('9.69');
  });

  test('汇总动作、主题样式和布尔标志', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(actionSummary(rule({ trigger_type: 'review_missing_timeout', actions: [{ action_type: 'send_text', message_template: '请评价', enabled: true }] }))).toBe('请评价');
    expect(actionSummary(rule({ trigger_type: 'review_missing_timeout', actions: [] }))).toBe('发送求评价文案');
    expect(actionSummary(rule({ actions: [] }))).toBe('未配置卡密库存');
    expect(actionSummary(rule({ actions: [
      { action_type: 'send_card', card_id: 1, card_name: '库存一', enabled: true },
      { action_type: 'send_card', card_id: 2, enabled: true },
      { action_type: 'send_text', message_template: '忽略', enabled: true },
    ] }))).toBe('库存一 / 卡密 2');
    expect(accentClasses('blue', true)).toContain('border-blue-500');
    expect(accentClasses('amber')).toContain('hover:border-amber-300');
    expect(accentClasses('unknown' as never)).toContain('border-blue-100');
    expect(statusPill(true)).toContain('bg-emerald-100');
    expect(statusPill(false)).toContain('bg-gray-200');
    expect(boolFlag(true)).toBe(true);
    expect(boolFlag(1)).toBe(true);
    expect(boolFlag('1')).toBe(true);
    expect(boolFlag(false)).toBe(false);
    expect(boolFlag('true')).toBe(false);
  });

  test('创建空规格并选择账号展示名称', /* 当前回调处理规则配置和展示状态。 */ () => {
    expect(emptyVariant()).toEqual(expect.objectContaining({ spec_name: '', spec_value: '', card_id: 0, delivery_count: 1, enabled: true, delay_override: false, delay_seconds: 0, source_type: 'local', goods_ref: '', goods_id: 0, profit_rate: '2.00', price_sync_enabled: false, stop_purchase_on_inversion: true }));
    // idOnly 是仅包含平台账号标识的最小账号对象。
    const idOnly = { id: 'account-1' } as AccountDetail;
    expect(accountLabel({ id: 'a', nickname: '昵称' } as AccountDetail)).toBe('昵称');
    expect(accountLabel({ id: 'a', remark: '备注' } as AccountDetail)).toBe('备注');
    expect(accountLabel(idOnly)).toBe('account-1');
    expect(accountLabel()).toBe('未知账号');
  });

  test('从商品 ID 和链接中提取货源商品标识', /* 当前回调验证货源商品输入兼容性。 */ () => {
    expect(fulfillmentGoodsID('40863')).toBe(40863);
    expect(fulfillmentGoodsID('４０８６３')).toBe(40863);
    expect(fulfillmentGoodsID('https://vip.zhikefa.com/goods?id=40863')).toBe(40863);
    expect(fulfillmentGoodsID('https://example.com/goods/40863/')).toBe(40863);
    expect(fulfillmentGoodsID('goods')).toBe(0);
  });
});
