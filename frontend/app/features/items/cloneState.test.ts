import { describe,expect,test } from 'vitest';
import type { Item } from './models';
import { buildItemCloneCSV,getItemCloneEligibility,itemCloneKey,normalizeClonePrice } from './cloneState';

// cloneableItem 是覆盖第一版成功克隆字段的单规格商品。
const cloneableItem: Item = {
  id: 1,
  cookie_id: 'source-account',
  item_id: 'item-1',
  item_title: '课程,资料',
  item_description: '第一行\n第二行',
  item_price: '￥1,299.00',
  item_image: 'https://example.com/item.jpg',
  is_multi_spec: false,
};

describe('商品跨账号克隆数据', /* cloneStateSuite 验证克隆资格和 CSV 映射。 */ () => {
  test('生成稳定选择键并归一化价格', /* identityTest 验证账号商品联合键和价格清洗。 */ () => {
    expect(itemCloneKey(cloneableItem)).toBe('source-account:item-1');
    expect(normalizeClonePrice(cloneableItem.item_price)).toBe('1299.00');
  });

  test('拒绝多规格、缺标题、缺主图和无效价格商品', /* eligibilityTest 验证第一版能力边界。 */ () => {
    expect(getItemCloneEligibility({ ...cloneableItem, is_multi_spec: true })).toEqual({ eligible: false, reason: '暂不支持多规格商品' });
    expect(getItemCloneEligibility({ ...cloneableItem, item_title: '' })).toEqual({ eligible: false, reason: '缺少商品标题' });
    expect(getItemCloneEligibility({ ...cloneableItem, item_image: '' })).toEqual({ eligible: false, reason: '缺少可用主图' });
    expect(getItemCloneEligibility({ ...cloneableItem, item_price: '免费' })).toEqual({ eligible: false, reason: '商品价格无效' });
    expect(getItemCloneEligibility(cloneableItem)).toEqual({ eligible: true, reason: '' });
  });

  test('生成目标账号批量预检 CSV 并正确转义内容', /* csvTest 验证克隆快照不会被逗号和换行破坏。 */ () => {
    // csv 是发送给现有批量预检接口的商品快照。
    const csv = buildItemCloneCSV([cloneableItem], 'target-account');
    expect(csv).toContain('"target-account","课程,资料","第一行\n第二行","1299.00","1","free","https://example.com/item.jpg"');
    expect(csv.split('\n')[0]).toBe('"账号ID","标题","描述","价格","库存","邮费模式","图片"');
  });
});
