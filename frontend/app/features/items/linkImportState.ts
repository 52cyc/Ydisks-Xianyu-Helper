import type { ItemLinkImportRow } from './models';

// MAX_ITEM_LINK_IMPORT_SOURCES 与服务端单次批量采集上限保持一致。
export const MAX_ITEM_LINK_IMPORT_SOURCES = 50;

// parseItemLinkSources 按行分割粘贴文本并丢弃空行，每行保留完整的闲鱼分享文案。
export const parseItemLinkSources = (value: string): string[] => value
  .split(/\r?\n/)
  .map(/* sourceNormalizer 移除单行分享文本的首尾空白。 */ source => source.trim())
  .filter(/* nonEmptySourcePredicate 排除不含分享内容的空行。 */ source => source.length > 0);

// escapeItemImportCSVCell 按 CSV 规则转义单个字段，避免文案中的逗号、引号或换行破坏表格。
const escapeItemImportCSVCell = (value: string | number): string => `"${String(value).replace(/"/g, '""')}"`;

// buildItemLinkImportCSV 将采集成功的单规格商品转换为现有批量上架预检可读的 CSV。
export const buildItemLinkImportCSV = (rows: ItemLinkImportRow[], targetAccountID: string): string => {
  // headers 是采集商品进入批量预检需要的字段集合。
  const headers = ['账号ID', '标题', '描述', '价格', '库存', '邮费模式', '图片', '采集源商品ID', '采集源链接'];
  // successfulRows 只保留有完整采集快照且没有失败说明的条目。
  const successfulRows = rows.filter(/* successfulRowPredicate 判断条目能否安全进入批量预检。 */ row => !row.error && row.item_id && row.title && row.price && row.images.length > 0);
  // csvRows 组合目标账号、采集快照和来源定位信息。
  const csvRows = successfulRows.map(/* importRowMapper 将一件采集商品映射为批量发布行。 */ row => [
    targetAccountID,
    row.title.trim(),
    (row.description || row.title).trim(),
    row.price.trim(),
    1,
    'free',
    row.images.join(';'),
    row.item_id,
    row.item_url,
  ]);
  return [headers, ...csvRows]
    .map(/* csvRowMapper 转义并连接当前 CSV 行。 */ row => row.map(/* csvCellMapper 转义当前 CSV 单元格。 */ escapeItemImportCSVCell).join(','))
    .join('\n');
};
