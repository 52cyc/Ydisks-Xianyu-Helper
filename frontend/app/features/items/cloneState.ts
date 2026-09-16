import type { Item } from './models';

// ItemCloneEligibility 描述源商品是否具备第一版跨账号克隆所需的数据。
export interface ItemCloneEligibility {
  // eligible 表示商品是否可以进入克隆预检。
  eligible: boolean;
  // reason 是商品不可克隆时展示给用户的原因。
  reason: string;
}

// itemCloneKey 返回商品在多账号列表中的稳定选择键。
export const itemCloneKey = (item: Item): string => `${item.cookie_id}:${item.item_id}`;

// normalizeClonePrice 将同步得到的价格文本转换为批量发布接受的数字格式。
export const normalizeClonePrice = (price?: string): string => String(price || '')
  .trim()
  .replace(/^[¥￥]\s*/, '')
  .replace(/,/g, '');

// getItemCloneEligibility 校验第一版克隆能够可靠继承的单规格商品字段。
export const getItemCloneEligibility = (item: Item): ItemCloneEligibility => {
  if (Boolean(item.is_multi_spec)) return { eligible: false, reason: '暂不支持多规格商品' };
  if (!String(item.item_title || '').trim()) return { eligible: false, reason: '缺少商品标题' };
  if (!String(item.item_image || '').trim()) return { eligible: false, reason: '缺少可用主图' };
  // price 是去除货币符号和千位分隔符后的数值。
  const price = Number(normalizeClonePrice(item.item_price));
  if (!Number.isFinite(price) || price <= 0) return { eligible: false, reason: '商品价格无效' };
  return { eligible: true, reason: '' };
};

// escapeCloneCSVCell 按 CSV 规则转义单个字段，避免标题和描述中的逗号或换行破坏表格。
const escapeCloneCSVCell = (value: string | number): string => `"${String(value).replace(/"/g, '""')}"`;

// buildItemCloneCSV 将已勾选商品转换为现有批量铺货预检能够直接读取的 CSV。
export const buildItemCloneCSV = (items: Item[], targetAccountID: string): string => {
  // headers 是克隆所需的最小批量发布字段集合。
  const headers = ['账号ID', '标题', '描述', '价格', '库存', '邮费模式', '图片', '克隆源账号ID', '克隆源商品ID'];
  // rows 将目标账号和源商品快照组合为待预检行。
  const rows = items.map(/* rowMapper 将一件源商品映射为目标账号的发布行。 */ item => [
    targetAccountID,
    String(item.item_title || '').trim(),
    String(item.item_description || item.item_title || '').trim(),
    normalizeClonePrice(item.item_price),
    1,
    'free',
    String(item.item_image || '').trim(),
    String(item.cookie_id || '').trim(),
    String(item.item_id || '').trim(),
  ]);
  return [headers, ...rows]
    .map(/* csvRowMapper 转义并连接当前 CSV 行。 */ row => row.map(/* csvCellMapper 转义当前 CSV 单元格。 */ escapeCloneCSVCell).join(','))
    .join('\n');
};
