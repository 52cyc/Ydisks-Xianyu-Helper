import { describe, expect, it } from "vitest";
import { buildFulfillmentBatchCSV, flattenCategoryLeaves, markedUpPrice } from "./catalogBatch";

/** 测试覆盖目录展开、向上取整加价和外部自动化字段写入。 */
describe("fulfillment catalog batch", () => {
  /** 用例验证三级目录在第二个选择器中展示完整叶子路径。 */
  it("flattens category leaves", () => {
    expect(flattenCategoryLeaves({ id: 1, name: "会员", children: [{ id: 2, name: "视频", children: [{ id: 3, name: "月卡" }] }] })).toEqual([{ id: 3, label: "视频 / 月卡" }]);
  });

  /** 用例验证售价不会因为浮点舍入低于目标百分比。 */
  it("rounds marked-up price upward", () => {
    expect(markedUpPrice("2.81", 10)).toBe("3.10");
  });

  /** 用例验证一件货源商品生成一条带外部规则字段的发布记录。 */
  it("builds one publish row per supplier product", /* buildCSVCase 验证选品行与外部规则字段。 */ () => {
    // csv 是一个可采购卡密商品生成的批量发布文本。
    const csv = buildFulfillmentBatchCSV([{ id: 9, goods_name: "测试卡", goods_img: "https://img.example/a.jpg", goods_type: 1, goods_price: "2.80", status: 1, stock_num: 8, can_buy: true }], 3, "account-1", 10);
    expect(csv).toContain('"external_goods_id"');
    expect(csv).toContain('"external_delivery_count"');
    expect(csv).toContain('"9"');
    expect(csv).toContain('"3.08"');
  });

  /** 用例验证供应站起购数会同步影响售价、库存和每件采购份数。 */
  it("applies supplier minimum quantity", /* minimumQuantityCase 验证起购数量贯穿发布字段。 */ () => {
    // csv 是起购两份、单次最多十份时生成的一条批量发布记录。
    const csv = buildFulfillmentBatchCSV([{ id: 9, goods_name: "测试卡", goods_img: "https://img.example/a.jpg", goods_type: 1, goods_price: "2.80", status: 1, stock_num: 13, can_buy: true, can_price: true, start_count: 2, end_count: 10 }], 3, "account-1", 10);
    // cells 是去掉 BOM 后解析出的数据行单元格。
    const cells = csv.replace(/^\uFEFF/, "").split("\r\n")[1].split(",").map(/* cell 是当前去除 CSV 外层引号的单元格。 */ cell => cell.slice(1, -1));
    expect(cells[3]).toBe("6.16");
    expect(cells[4]).toBe("5");
    expect(cells[12]).toBe("2");
    expect(cells[13]).toBe("5.60");
  });

  /** 用例验证多份售价与后台同步一样先按单份向上取整，避免首次同步后立刻涨价。 */
  it("rounds each supplier unit before multiplying", /* unitRoundingCase 验证单份舍入顺序。 */ () => {
    // csv 是一分钱商品起购两份时生成的批量发布文本。
    const csv = buildFulfillmentBatchCSV([{ id: 10, goods_name: "低价卡", goods_img: "https://img.example/b.jpg", goods_type: 1, goods_price: "0.01", status: 1, stock_num: 20, can_buy: true, start_count: 2 }], 3, "account-1", 10);
    // priceCell 是发布记录中的闲鱼初始售价。
    const priceCell = csv.replace(/^\uFEFF/, "").split("\r\n")[1].split(",")[3];
    expect(priceCell).toBe('"0.04"');
  });

  /** 用例验证发布前详情库存未知或为零时不会生成上架记录。 */
  it("rejects non-positive detail stock", /* rejectStockCase 验证详情库存必须是正数。 */ () => {
    expect(/* buildUnknownStockCSV 尝试使用详情阶段仍未知的库存生成发布记录。 */ () => buildFulfillmentBatchCSV([{ id: 9, goods_name: "测试卡", goods_img: "https://img.example/a.jpg", goods_type: 1, goods_price: "2.80", status: 1, stock_num: -1, can_buy: true }], 3, "account-1", 10)).toThrow("当前不可采购");
  });
});
