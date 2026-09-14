import type { FulfillmentCategory, FulfillmentProduct } from "./api";

/** FulfillmentCategoryOption 是二级选择器展示的最终可查询目录路径。 */
export interface FulfillmentCategoryOption {
  /** id 是传给货源商品列表接口的叶子目录标识。 */
  id: number;
  /** label 是从一级目录以下拼接出的完整目录路径。 */
  label: string;
}

/** flattenCategoryLeaves 把一个一级目录下的所有叶子节点展开为可选路径。 */
export function flattenCategoryLeaves(category: FulfillmentCategory): FulfillmentCategoryOption[] {
  /** visit 递归收集目录叶子；node 是当前节点，parents 是不含一级目录的上级名称。 */
  const visit = (node: FulfillmentCategory, parents: string[]): FulfillmentCategoryOption[] => {
    // path 保存当前节点相对一级目录的完整名称路径。
    const path = [...parents, node.name];
    if (!node.children?.length) return [{ id: node.id, label: path.join(" / ") }];
    return node.children.flatMap(/* child 是当前继续展开的下级目录。 */ child => visit(child, path));
  };
  if (!category.children?.length) return [{ id: category.id, label: category.name }];
  return category.children.flatMap(/* child 是一级目录下当前待展开的节点。 */ child => visit(child, []));
}

/** markedUpPrice 按百分比向上取整到分，避免小数舍入造成售价低于目标利润。 */
export function markedUpPrice(sourcePrice: string, profitRate: number): string {
  // price 是货源采购价的数字形式。
  const price = Number(sourcePrice);
  if (!Number.isFinite(price) || price <= 0) throw new Error("货源采购价无效");
  if (!Number.isFinite(profitRate) || profitRate < 0 || profitRate > 1000) throw new Error("加价比例必须在 0 到 1000% 之间");
  // cents 是向上取整后的闲鱼售价分值。
  const cents = Math.ceil(price * (1 + profitRate / 100) * 100 - 1e-8);
  return (cents / 100).toFixed(2);
}

/** csvCell 对 CSV 单元格进行引号转义，保留商品详情中的换行和逗号。 */
function csvCell(value: string | number | boolean): string {
  // text 是待写入 CSV 的稳定文本。
  const text = String(value);
  return `"${text.replaceAll('"', '""')}"`;
}

/** buildFulfillmentBatchCSV 把每个货源商品转换为一条现有批量发布记录和一个外部发货规则。 */
export function buildFulfillmentBatchCSV(products: FulfillmentProduct[], instanceId: number, accountId: string, profitRate: number): string {
  if (!accountId) throw new Error("请选择闲鱼发布账号");
  if (instanceId <= 0) throw new Error("请选择货源实例");
  if (products.length === 0) throw new Error("请至少选择一个货源商品");
  // headers 是批量预检和外部自动化解析器共同识别的稳定字段。
  const headers = [
    "cookie_id", "title", "description", "price", "quantity", "postage_mode", "images",
    "external_delivery_enabled", "external_instance_id", "external_goods_id", "external_goods_name",
    "external_goods_type", "external_delivery_count", "external_safe_price", "external_profit_rate", "external_price_sync_enabled",
    "external_stop_purchase_on_inversion",
  ];
  // rows 保存标题行以及每个单规格货源商品对应的数据行。
  const rows = [headers.map(csvCell).join(",")];
  for (const /* product 是当前转换为闲鱼商品的单规格货源商品。 */ product of products) {
    if (product.goods_type !== 1) throw new Error(`商品 #${product.id} 不是首版支持的卡密类型`);
    if (!product.can_buy || product.status !== 1 || product.stock_num <= 0) throw new Error(`商品 #${product.id} 当前不可采购`);
    if (!product.goods_img) throw new Error(`商品 #${product.id} 缺少主图`);
    // deliveryCount 是每卖出一件闲鱼商品必须向供应站采购的最小份数。
    const deliveryCount = Math.max(1, product.start_count || 1);
    if (product.end_count && deliveryCount > product.end_count) throw new Error(`商品 #${product.id} 的起购数量超过单次限购数量`);
    // description 合并货源详情和购买须知，空详情回退商品名称。
    const description = [product.goods_info, product.goods_notice].filter(Boolean).join("\n\n") || product.goods_name;
    // stockUnits 是按起购份数折算后的闲鱼可售件数。
    const stockUnits = Math.floor(product.stock_num / deliveryCount);
    // orderLimitUnits 是供应站单次限购数量折算出的闲鱼单笔最大件数；零表示没有声明上限。
    const orderLimitUnits = product.end_count ? Math.floor(product.end_count / deliveryCount) : 999;
    // quantity 同时受供应站库存、单次限购和闲鱼批量发布上限约束。
    const quantity = Math.min(stockUnits, orderLimitUnits, 999);
    if (quantity <= 0) throw new Error(`商品 #${product.id} 库存不足起购数量`);
    // supplierTotalPrice 是每件闲鱼商品对应的供应商采购总价。
    const supplierTotalPrice = Number(product.goods_price || "") * deliveryCount;
    // listingPrice 与后台价格同步保持一致：先把单份加价向上取整到分，再乘采购份数。
    const listingPrice = (Number(markedUpPrice(product.goods_price || "", profitRate)) * deliveryCount).toFixed(2);
    // values 与 headers 顺序一一对应，供现有批量预检直接消费。
    const values: Array<string | number | boolean> = [
      accountId, product.goods_name, description, listingPrice, quantity,
      "free", product.goods_img, true, instanceId, product.id, product.goods_name, product.goods_type,
      deliveryCount, product.can_price ? supplierTotalPrice.toFixed(2) : "", profitRate.toFixed(2), true, true,
    ];
    rows.push(values.map(csvCell).join(","));
  }
  return `\uFEFF${rows.join("\r\n")}`;
}
