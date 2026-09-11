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
    expect(csv).toContain('"9"');
    expect(csv).toContain('"3.08"');
  });
});
