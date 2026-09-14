import { LoaderCircle, PackagePlus, Search } from "lucide-react";
import React, { useEffect, useRef, useState } from "react";
import { DEFAULT_BATCH_PUBLISH_INTERVAL_SECONDS } from "../../../../shared/config/publish";
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
  type FulfillmentPublishAccount,
} from "../api";
import { buildFulfillmentBatchCSV, flattenCategoryLeaves } from "../catalogBatch";
import { providerLabel } from "../provider";

/** CatalogBatchPanelProps 是货源选品面板所需的实例列表。 */
interface CatalogBatchPanelProps {
  /** instances 是当前用户已经配置的全部货源实例。 */
  instances: FulfillmentInstance[];
}

/** CatalogBatchPanel 完成支持目录协议的货源选品、统一加价、预检和批量上架。 */
export const CatalogBatchPanel: React.FC<CatalogBatchPanelProps> = ({ instances }) => {
  // accounts、setAccounts 保存可用于发布的非敏感闲鱼账号选项。
  const [accounts, setAccounts] = useState<FulfillmentPublishAccount[]>([]);
  // accountId、setAccountId 保存本批次统一使用的闲鱼账号。
  const [accountId, setAccountId] = useState("");
  // instanceId、setInstanceId 保存本次选品使用的目录型货源实例。
  const [instanceId, setInstanceId] = useState(0);
  // categories、setCategories 保存当前实例的远程目录树。
  const [categories, setCategories] = useState<FulfillmentCategory[]>([]);
  // topCategoryId、setTopCategoryId 保存一级目录选择。
  const [topCategoryId, setTopCategoryId] = useState(0);
  // leafCategoryId、setLeafCategoryId 保存最终用于查询商品的叶子目录。
  const [leafCategoryId, setLeafCategoryId] = useState(0);
  // keyword、setKeyword 保存商品名称搜索词。
  const [keyword, setKeyword] = useState("");
  // products、setProducts 保存当前远程商品页。
  const [products, setProducts] = useState<FulfillmentProduct[]>([]);
  // selectedIds、setSelectedIds 保存当前跨页选择的货源商品标识。
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  // page、setPage 保存从一开始的当前页码。
  const [page, setPage] = useState(1);
  // total、setTotal 保存当前筛选条件的远程商品总数。
  const [total, setTotal] = useState(0);
  // totalPages、setTotalPages 保存供应站声明的总页数，缺失时兼容按页大小推导。
  const [totalPages, setTotalPages] = useState(1);
  // profitRate、setProfitRate 保存统一加价百分比。
  const [profitRate, setProfitRate] = useState(10);
  // publishInterval、setPublishInterval 保存相邻两次闲鱼发布的最小间隔秒数。
  const [publishInterval, setPublishInterval] = useState(DEFAULT_BATCH_PUBLISH_INTERVAL_SECONDS);
  // loading、setLoading 标记目录或商品请求正在进行。
  const [loading, setLoading] = useState(false);
  // publishing、setPublishing 标记预检和启动发布正在进行。
  const [publishing, setPublishing] = useState(false);
  // message、setMessage 保存面板内的成功或失败反馈。
  const [message, setMessage] = useState("");
  // requestGeneration 使切换实例后的晚到目录响应失效。
  const requestGeneration = useRef(0);

  // catalogInstances 是首版允许进入目录批量上架流程的已启用卡速售或卡易信实例。
  const catalogInstances = instances.filter(/* instance 是当前待筛选的货源实例。 */ instance => (instance.provider === "kasushou_v2" || instance.provider === "kayixin_v3") && instance.enabled);
  // topCategory 是当前一级目录对应的完整节点。
  const topCategory = categories.find(/* category 是当前待匹配的一级目录。 */ category => category.id === topCategoryId);
  // leafOptions 是一级目录下压平后的最终目录选项。
  const leafOptions = topCategory ? flattenCategoryLeaves(topCategory) : [];
  // pageCount 优先采用供应站页数，旧服务响应缺失时按实际页大小兼容推导。
  const pageCount = Math.max(1, totalPages);

  useEffect(/* accountLoadEffect 读取账号选择项并在卸载后丢弃晚到响应。 */ () => {
    // active 表示当前 effect 仍允许提交请求结果。
    let active = true;
    void listFulfillmentPublishAccounts().then(/* loadedAccounts 是服务端返回的非敏感账号列表。 */ loadedAccounts => {
      if (!active) return;
      // enabledAccounts 只保留当前允许发布的账号。
      const enabledAccounts = loadedAccounts.filter(/* account 是当前待筛选的账号。 */ account => account.enabled);
      setAccounts(enabledAccounts);
      setAccountId(/* current 是用户可能已经选择的账号。 */ current => current || enabledAccounts[0]?.id || "");
    }).catch(/* loadError 是账号列表读取失败原因。 */ loadError => {
      if (active) setMessage(loadError instanceof Error ? loadError.message : "闲鱼账号加载失败");
    });
    return /* accountLoadCleanup 禁止页面卸载后的响应覆盖状态。 */ () => { active = false; };
  }, []);

  /** loadCategories 切换目录型货源实例后读取目录并清空旧选品。 */
  const loadCategories = async (nextInstanceId: number): Promise<void> => {
    // generation 是本次目录请求的代次标识。
    const generation = ++requestGeneration.current;
    setInstanceId(nextInstanceId);
    setCategories([]);
    setTopCategoryId(0);
    setLeafCategoryId(0);
    setProducts([]);
    setSelectedIds(new Set());
    setTotal(0);
    setTotalPages(1);
    if (!nextInstanceId) return;
    setLoading(true);
    try {
      // loadedCategories 是当前货源实例的完整目录树。
      const loadedCategories = await listFulfillmentCategories(nextInstanceId);
      if (generation !== requestGeneration.current) return;
      setCategories(loadedCategories);
    } catch (loadError /* loadError 是远程目录读取失败原因。 */) {
      if (generation === requestGeneration.current) setMessage(loadError instanceof Error ? loadError.message : "货源目录加载失败");
    } finally {
      if (generation === requestGeneration.current) setLoading(false);
    }
  };

  /** loadProducts 按当前叶子目录、关键词和页码读取商品。 */
  const loadProducts = async (nextPage: number): Promise<void> => {
    if (!instanceId || !leafCategoryId) {
      setMessage("请先选择一级目录和商品目录");
      return;
    }
    // generation 是本次商品查询的代次，切换目录或重复查询会使旧响应失效。
    const generation = ++requestGeneration.current;
    setLoading(true);
    setMessage("");
    try {
      // result 是当前筛选条件的商品分页响应。
      const result = await listFulfillmentProductPage(instanceId, leafCategoryId, keyword.trim(), nextPage, 50);
      if (generation !== requestGeneration.current) return;
      setProducts(result.data);
      setTotal(result.total);
      setPage(result.page);
      setTotalPages(result.total_pages ?? Math.max(1, Math.ceil(result.total / Math.max(1, result.page_size))));
    } catch (loadError /* loadError 是商品页读取失败原因。 */) {
      if (generation === requestGeneration.current) setMessage(loadError instanceof Error ? loadError.message : "货源商品加载失败");
    } finally {
      if (generation === requestGeneration.current) setLoading(false);
    }
  };

  /** toggleProduct 切换一个可采购卡密商品的跨页选中状态。 */
  const toggleProduct = (product: FulfillmentProduct): void => {
    setSelectedIds(/* currentIds 是事件触发时最新的跨页选择集合。 */ currentIds => {
      // nextIds 是基于最新选择复制出的可变集合。
      const nextIds = new Set(currentIds);
      if (nextIds.has(product.id)) nextIds.delete(product.id);
      else nextIds.add(product.id);
      return nextIds;
    });
  };

  /** publishSelected 在发布前逐个复核详情，然后复用现有批次预检和启动接口。 */
  const publishSelected = async (): Promise<void> => {
    if (selectedIds.size > 50) {
      setMessage("单个批次最多选择 50 个商品");
      return;
    }
    setPublishing(true);
    setMessage("");
    try {
      // details 按选择顺序逐个复核，避免浏览器同时向同一供应站发起多条详情请求。
      const details: FulfillmentProduct[] = [];
      for (const /* goodsId 是当前待复核的货源商品标识。 */ goodsId of selectedIds) {
        // detail 是发布前重新读取的实时价格、库存和状态。
        const detail = await getFulfillmentProduct(instanceId, goodsId);
        details.push(detail);
      }
      // csv 是现有批量预检接口可以直接解析的 UTF-8 表格内容。
      const csv = buildFulfillmentBatchCSV(details, instanceId, accountId, profitRate);
      // file 是只存在于本次请求内存中的选品表格，不包含货源密钥。
      const file = new File([csv], "supplier-products.csv", { type: "text/csv;charset=utf-8" });
      // preview 是逐行校验并持久化后的现有批次预检结果。
      const preview = await previewFulfillmentPublishBatch(file, accountId, publishInterval);
      if (preview.invalid > 0) {
        // firstError 是第一条未通过预检的用户可见原因。
        const firstError = preview.rows.find(/* row 是当前待定位错误的预检行。 */ row => !row.valid)?.errors?.join("；") || "存在未通过预检的商品";
        setMessage(`预检未通过：${firstError}`);
        return;
      }
      // started 是后台批量发布任务的稳定标识。
      const started = await startFulfillmentPublishBatch(preview.preview_id);
      setMessage(`已开始上架 ${preview.valid} 个商品，批次：${started.batch_id}`);
      setSelectedIds(new Set());
    } catch (publishError /* publishError 是详情复核、预检或启动发布的失败原因。 */) {
      setMessage(publishError instanceof Error ? publishError.message : "批量上架失败");
    } finally {
      setPublishing(false);
    }
  };

  return (
    <section className="ios-card overflow-hidden rounded-2xl bg-white shadow-lg">
      <div className="border-b border-slate-100 p-5">
        <h3 className="flex items-center gap-2 font-extrabold text-slate-900"><PackagePlus className="h-5 w-5 text-sky-600" /> 货源商品批量上架</h3>
        <p className="mt-1 text-sm text-slate-500">首版仅支持单规格卡密商品：一个货源商品对应一个闲鱼商品，发布成功后自动关联付款采购与发货。</p>
      </div>
      <div className="grid gap-4 p-5 md:grid-cols-3">
        <label className="space-y-1 text-sm font-bold text-slate-700">闲鱼账号
          <select className="ios-input w-full rounded-xl" value={accountId} onChange={/* accountChangeHandler 切换本批次发布账号。 */ event => setAccountId(event.target.value)}><option value="">请选择</option>{accounts.map(/* account 是当前账号选项。 */ account => <option key={account.id} value={account.id}>{account.name}</option>)}</select>
        </label>
        <label className="space-y-1 text-sm font-bold text-slate-700">货源实例
          <select className="ios-input w-full rounded-xl" value={instanceId} onChange={/* instanceChangeHandler 切换实例并重新加载目录。 */ event => void loadCategories(Number(event.target.value))}><option value={0}>请选择</option>{catalogInstances.map(/* instance 是当前支持目录选品的货源实例。 */ instance => <option key={instance.id} value={instance.id}>{instance.name}（{providerLabel(instance.provider)}）</option>)}</select>
        </label>
        <label className="space-y-1 text-sm font-bold text-slate-700">一级目录
          <select className="ios-input w-full rounded-xl" value={topCategoryId} onChange={/* topCategoryChangeHandler 切换一级目录并重置叶子目录。 */ event => { setTopCategoryId(Number(event.target.value)); setLeafCategoryId(0); }}><option value={0}>请选择</option>{categories.map(/* category 是当前一级目录选项。 */ category => <option key={category.id} value={category.id}>{category.name}</option>)}</select>
        </label>
        <label className="space-y-1 text-sm font-bold text-slate-700">商品目录
          <select className="ios-input w-full rounded-xl" value={leafCategoryId} onChange={/* leafCategoryChangeHandler 选择最终查询目录。 */ event => setLeafCategoryId(Number(event.target.value))}><option value={0}>请选择</option>{leafOptions.map(/* option 是当前叶子目录路径。 */ option => <option key={option.id} value={option.id}>{option.label}</option>)}</select>
        </label>
        <label className="space-y-1 text-sm font-bold text-slate-700">统一加价比例（%）
          <input className="ios-input w-full rounded-xl" type="number" min="0" max="1000" step="0.01" value={profitRate} onChange={/* profitRateChangeHandler 更新售价加价比例。 */ event => setProfitRate(Number(event.target.value))} />
        </label>
        <label className="space-y-1 text-sm font-bold text-slate-700">发布间隔（秒）
          <input className="ios-input w-full rounded-xl" type="number" min="1" max="3600" value={publishInterval} onChange={/* intervalChangeHandler 更新发布节流间隔。 */ event => setPublishInterval(Number(event.target.value))} />
        </label>
        <div className="flex gap-2 md:col-span-3">
          <input className="ios-input min-w-0 flex-1 rounded-xl" value={keyword} placeholder="按商品名称搜索，可留空" onChange={/* keywordChangeHandler 更新商品搜索词。 */ event => setKeyword(event.target.value)} />
          <button type="button" disabled={loading} onClick={/* searchHandler 从第一页加载当前筛选商品。 */ () => void loadProducts(1)} className="ios-btn-primary min-h-11 rounded-xl px-5 font-bold"><Search className="mr-1 inline h-4 w-4" />查询商品</button>
        </div>
      </div>
      {message && <div role="status" className="mx-5 mb-4 rounded-xl bg-sky-50 px-4 py-3 text-sm font-medium text-sky-800">{message}</div>}
      <div className="overflow-x-auto border-t border-slate-100">
        <table className="w-full min-w-[760px] text-left text-sm"><thead className="bg-slate-50 text-xs text-slate-500"><tr><th className="px-5 py-3">选择</th><th className="px-5 py-3">货源商品</th><th className="px-5 py-3">采购价</th><th className="px-5 py-3">库存</th><th className="px-5 py-3">状态</th></tr></thead>
          <tbody className="divide-y divide-slate-100">{loading && <tr><td colSpan={5} className="p-8 text-center"><LoaderCircle className="mx-auto h-6 w-6 animate-spin text-sky-600" /></td></tr>}{!loading && products.length === 0 && <tr><td colSpan={5} className="p-8 text-center text-slate-500">选择目录后查询可上架商品。</td></tr>}{!loading && products.map(/* product 是当前商品表格行。 */ product => {
            // eligible 表示商品是首版允许上架的在售有库存卡密商品。
            const eligible = product.goods_type === 1 && product.can_buy && product.status === 1 && product.stock_num !== 0 && Boolean(product.goods_img);
            return <tr key={product.id}><td className="px-5 py-4"><input aria-label={`选择 ${product.goods_name}`} type="checkbox" disabled={!eligible} checked={selectedIds.has(product.id)} onChange={/* productToggleHandler 切换当前货源商品。 */ () => toggleProduct(product)} /></td><td className="px-5 py-4"><div className="font-bold text-slate-900">{product.goods_name}</div><div className="text-xs text-slate-500">#{product.id} · {product.goods_type === 1 ? "卡密" : "直充（首版不支持）"}</div></td><td className="px-5 py-4">¥{product.goods_price || "—"}</td><td className="px-5 py-4">{product.stock_num < 0 ? "详情确认" : product.stock_num}</td><td className="px-5 py-4">{eligible ? <span className="text-emerald-700">可上架</span> : <span className="text-slate-400">暂不可选</span>}</td></tr>;
          })}</tbody></table>
      </div>
      <div className="flex flex-col gap-3 border-t border-slate-100 p-5 sm:flex-row sm:items-center sm:justify-between"><div className="text-sm text-slate-500">第 {page}/{pageCount} 页，共 {total} 个；已选 {selectedIds.size} 个</div><div className="flex gap-2"><button type="button" disabled={page <= 1 || loading} onClick={/* previousPageHandler 读取上一页商品。 */ () => void loadProducts(page - 1)} className="min-h-11 rounded-xl border px-4 font-bold">上一页</button><button type="button" disabled={page >= pageCount || loading} onClick={/* nextPageHandler 读取下一页商品。 */ () => void loadProducts(page + 1)} className="min-h-11 rounded-xl border px-4 font-bold">下一页</button><button type="button" disabled={publishing || selectedIds.size === 0 || !accountId} onClick={/* publishHandler 复核并启动选中商品批量上架。 */ () => void publishSelected()} className="ios-btn-primary min-h-11 rounded-xl px-5 font-bold disabled:opacity-50">{publishing ? "正在预检…" : `批量上架（${selectedIds.size}）`}</button></div></div>
    </section>
  );
};
