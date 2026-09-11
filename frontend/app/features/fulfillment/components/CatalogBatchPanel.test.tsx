// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  getFulfillmentProduct,
  listFulfillmentCategories,
  listFulfillmentProductPage,
  listFulfillmentPublishAccounts,
  previewFulfillmentPublishBatch,
  startFulfillmentPublishBatch,
  type FulfillmentCategory,
  type FulfillmentInstance,
  type FulfillmentProduct,
} from "../api";
import { CatalogBatchPanel } from "./CatalogBatchPanel";

vi.mock("../api", /* fulfillmentAPIMock 提供不访问网络的选品与发布接口。 */ () => ({
  getFulfillmentProduct: vi.fn(),
  listFulfillmentCategories: vi.fn(),
  listFulfillmentProductPage: vi.fn(),
  listFulfillmentPublishAccounts: vi.fn(),
  previewFulfillmentPublishBatch: vi.fn(),
  startFulfillmentPublishBatch: vi.fn(),
}));

/** product 是所有页面行为测试共用的可采购单规格卡密商品。 */
const product = { id: 9, goods_name: "视频月卡", goods_img: "https://img.example/a.jpg", goods_type: 1, goods_price: "2.80", status: 1, stock_num: 8, can_buy: true, can_price: true } as FulfillmentProduct;
/** instances 是页面行为测试使用的两个卡速售实例。 */
const instances = [
  { id: 1, public_id: "one", name: "卡速售一", provider: "kasushou_v2", base_url: "https://one.example", merchant_user_id: "u1", has_api_key: true, enabled: true, capabilities: {}, created_at: "", updated_at: "" },
  { id: 2, public_id: "two", name: "卡速售二", provider: "kasushou_v2", base_url: "https://two.example", merchant_user_id: "u2", has_api_key: true, enabled: true, capabilities: {}, created_at: "", updated_at: "" },
] as FulfillmentInstance[];

/** CatalogBatchPanel 行为测试覆盖正常发布、预检失败和实例切换隔离。 */
describe("CatalogBatchPanel", () => {
  beforeEach(/* resetMocks 为每个用例设置独立的账号和发布接口结果。 */ () => {
    vi.resetAllMocks();
    vi.mocked(listFulfillmentPublishAccounts).mockResolvedValue([{ id: "acc1", name: "主账号", enabled: true }]);
    vi.mocked(getFulfillmentProduct).mockResolvedValue(product);
    vi.mocked(startFulfillmentPublishBatch).mockResolvedValue({ success: true, batch_id: "batch-1" });
  });

  afterEach(/* cleanupTimers 恢复可能由失败用例修改的计时器状态。 */ () => {
	cleanup();
    vi.useRealTimers();
  });

  it("starts the existing publish batch after successful preflight", /* successCase 验证选品到启动发布的完整成功路径。 */ async () => {
    vi.mocked(listFulfillmentCategories).mockResolvedValue([{ id: 10, name: "会员", children: [{ id: 11, name: "视频" }] }]);
    vi.mocked(listFulfillmentProductPage).mockResolvedValue({ data: [product], total: 1, page: 1, page_size: 50 });
    vi.mocked(previewFulfillmentPublishBatch).mockResolvedValue({ success: true, preview_id: "preview-1", total: 1, valid: 1, invalid: 0, rows: [] });
    render(<CatalogBatchPanel instances={instances} />);
    await waitFor(/* accountReadyAssertion 等待默认发布账号加载。 */ () => expect((screen.getByLabelText("闲鱼账号") as HTMLSelectElement).value).toBe("acc1"));
    fireEvent.change(screen.getByLabelText("卡速售实例"), { target: { value: "1" } });
    await waitFor(/* categoryReadyAssertion 等待一级目录加载。 */ () => expect(screen.getByRole("option", { name: "会员" })).toBeTruthy());
    fireEvent.change(screen.getByLabelText("一级目录"), { target: { value: "10" } });
    fireEvent.change(screen.getByLabelText("商品目录"), { target: { value: "11" } });
    fireEvent.click(screen.getByRole("button", { name: /查询商品/ }));
    await waitFor(/* productReadyAssertion 等待商品列表加载。 */ () => expect(screen.getByText("视频月卡")).toBeTruthy());
    fireEvent.click(screen.getByLabelText("选择 视频月卡"));
    fireEvent.click(screen.getByRole("button", { name: "批量上架（1）" }));
    await waitFor(/* publishStartedAssertion 等待现有批次启动完成。 */ () => expect(screen.getByText(/batch-1/)).toBeTruthy());
    expect(previewFulfillmentPublishBatch).toHaveBeenCalledOnce();
    expect(startFulfillmentPublishBatch).toHaveBeenCalledWith("preview-1");
  });

  it("keeps an invalid preview stopped", /* invalidCase 验证任一行预检失败时不会启动后台发布。 */ async () => {
    vi.mocked(listFulfillmentCategories).mockResolvedValue([{ id: 10, name: "会员", children: [{ id: 11, name: "视频" }] }]);
    vi.mocked(listFulfillmentProductPage).mockResolvedValue({ data: [product], total: 1, page: 1, page_size: 50 });
    vi.mocked(previewFulfillmentPublishBatch).mockResolvedValue({ success: true, preview_id: "preview-bad", total: 1, valid: 0, invalid: 1, rows: [{ row_no: 2, valid: false, errors: ["类目识别失败"], cookie_id: "acc1", title: "视频月卡", price: "3.08", quantity: 8, images: [], category: { cat_id: "", cat_name: "", channel_cat_id: "" }, automation: { paid_delivery: { enabled: false, actions: [] }, review_gift: { enabled: false, actions: [] }, review_request: { enabled: false, after_shipped_hours: 72, message: "", max_attempts: 1, delay_seconds: 0 } } }] });
    render(<CatalogBatchPanel instances={instances} />);
    await waitFor(/* accountReadyAssertion 等待账号加载后再执行选品。 */ () => expect((screen.getByLabelText("闲鱼账号") as HTMLSelectElement).value).toBe("acc1"));
    fireEvent.change(screen.getByLabelText("卡速售实例"), { target: { value: "1" } });
    await waitFor(/* categoryReadyAssertion 等待目录加载。 */ () => expect(screen.getByRole("option", { name: "会员" })).toBeTruthy());
    fireEvent.change(screen.getByLabelText("一级目录"), { target: { value: "10" } });
    fireEvent.change(screen.getByLabelText("商品目录"), { target: { value: "11" } });
    fireEvent.click(screen.getByRole("button", { name: /查询商品/ }));
    await waitFor(/* productReadyAssertion 等待可选择商品出现。 */ () => expect(screen.getByLabelText("选择 视频月卡")).toBeTruthy());
    fireEvent.click(screen.getByLabelText("选择 视频月卡"));
    fireEvent.click(screen.getByRole("button", { name: "批量上架（1）" }));
    await waitFor(/* invalidMessageAssertion 等待逐行预检错误展示。 */ () => expect(screen.getByText(/类目识别失败/)).toBeTruthy());
    expect(startFulfillmentPublishBatch).not.toHaveBeenCalled();
  });

  it("ignores categories returned by an older instance request", /* staleCase 验证快速切换实例不会被旧目录响应覆盖。 */ async () => {
    // resolveFirst 保存第一个实例目录请求的延迟完成函数。
    let resolveFirst: ((value: FulfillmentCategory[]) => void) | undefined;
    // firstRequest 是直到测试显式完成才返回的旧实例目录请求。
    const firstRequest = new Promise<FulfillmentCategory[]>(/* firstResolver 捕获旧请求完成函数。 */ resolve => { resolveFirst = resolve; });
    vi.mocked(listFulfillmentCategories).mockImplementation(/* categoryRequestMock 按实例返回延迟或即时目录。 */ instanceId => instanceId === 1 ? firstRequest : Promise.resolve([{ id: 20, name: "新目录" }]));
    render(<CatalogBatchPanel instances={instances} />);
    fireEvent.change(screen.getByLabelText("卡速售实例"), { target: { value: "1" } });
    fireEvent.change(screen.getByLabelText("卡速售实例"), { target: { value: "2" } });
    await waitFor(/* latestCategoryAssertion 等待第二个实例目录显示。 */ () => expect(screen.getByRole("option", { name: "新目录" })).toBeTruthy());
    resolveFirst?.([{ id: 10, name: "旧目录" }]);
    await Promise.resolve();
    expect(screen.queryByRole("option", { name: "旧目录" })).toBeNull();
  });
});
