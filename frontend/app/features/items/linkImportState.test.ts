import { describe,expect,test } from 'vitest';
import type { ItemLinkImportRow } from './models';
import { buildItemLinkImportCSV,parseItemLinkSources } from './linkImportState';

// collectedRow 是覆盖批量预检映射的成功采集结果。
const collectedRow: ItemLinkImportRow = {
  row_no: 1,
  source: '【闲鱼】https://m.tb.cn/example',
  item_id: '1073801913752',
  item_url: 'https://www.goofish.com/item?id=1073801913752',
  title: '课程,资料',
  description: '第一行\n第二行',
  price: '19.90',
  images: ['https://example.com/1.jpg', 'https://example.com/2.jpg'],
  error: '',
};

describe('商品分享链接批量采集数据', /* linkImportStateSuite 验证粘贴文本解析和 CSV 映射。 */ () => {
  test('按行保留完整分享文案并忽略空行', /* sourceParsingTest 验证每行作为一条采集源。 */ () => {
    expect(parseItemLinkSources('  【闲鱼】 https://m.tb.cn/a  \n\nhttps://www.goofish.com/item?id=2\r\n')).toEqual([
      '【闲鱼】 https://m.tb.cn/a',
      'https://www.goofish.com/item?id=2',
    ]);
  });

  test('只将成功条目生成批量预检 CSV', /* csvMappingTest 验证失败行过滤、图片连接和特殊字符转义。 */ () => {
    // failedRow 代表同一批次中已隔离的失败条目。
    const failedRow: ItemLinkImportRow = { ...collectedRow, row_no: 2, item_id: '', title: '', images: [], error: '链接无效' };
    // csv 是将成功条目送入现有批量预检的文件内容。
    const csv = buildItemLinkImportCSV([collectedRow, failedRow], 'target-account');
    expect(csv.split('\n')[0]).toBe('"账号ID","标题","描述","价格","库存","邮费模式","图片","采集源商品ID","采集源链接"');
    expect(csv).toContain('"target-account","课程,资料","第一行\n第二行","19.90","1","free","https://example.com/1.jpg;https://example.com/2.jpg"');
    expect(csv).not.toContain('链接无效');
  });
});
