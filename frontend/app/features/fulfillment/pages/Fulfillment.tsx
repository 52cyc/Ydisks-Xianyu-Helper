import {
  Boxes,
  KeyRound,
  LoaderCircle,
  PackageSearch,
  Plus,
  RefreshCw,
  ShieldCheck,
  ShoppingCart,
  Trash2,
  X,
} from "lucide-react";
import React, { useEffect, useState } from "react";
import {
  createFulfillmentInstance,
  deleteFulfillmentInstance,
  listFulfillmentInstances,
  listFulfillmentOrders,
  purchaseFulfillmentOrder,
  refreshFulfillmentOrder,
  type FulfillmentInstance,
  type FulfillmentInstanceInput,
  type FulfillmentOrder,
} from "../api";
import {
  merchantCredentialLabel,
  providerCapabilities,
  providerLabel,
  secretCredentialLabel,
  type FulfillmentProvider,
} from "../provider";

/** emptyInstance 是新货源实例表单的默认值。 */
const emptyInstance: FulfillmentInstanceInput = {
  name: "",
  provider: "kasushou_v2",
  base_url: "",
  merchant_user_id: "",
  api_key: "",
  enabled: true,
  capabilities: {
    order_list: false,
    order_callback: true,
    cancel_callback: false,
    cancel_request_mode: "callback_url",
    card_show_type: false,
  },
};

/** stateLabel 把履约状态转为中文展示。 */
const stateLabel = (state: string): string =>
  ({
    created: "已建单",
    unpaid: "未支付",
    waiting: "等待处理",
    processing: "处理中",
    succeeded: "已成功",
    failed: "已失败",
    cancelled: "已取消",
    refunded: "已退款",
    unknown: "未知",
  })[state] ?? state;

/** stateClass 返回履约状态对应的可读颜色。 */
const stateClass = (state: string): string =>
  state === "succeeded"
    ? "bg-emerald-50 text-emerald-700"
    : state === "failed" || state === "cancelled" || state === "refunded"
      ? "bg-red-50 text-red-700"
      : "bg-amber-50 text-amber-700";

/** Fulfillment 渲染多协议货源实例和履约订单页面。 */
const Fulfillment: React.FC = () => {
  // instances 是当前用户配置的货源站列表。
  const [instances, setInstances] = useState<FulfillmentInstance[]>([]);
  // orders 是最新外部货源履约订单。
  const [orders, setOrders] = useState<FulfillmentOrder[]>([]);
  // instanceForm 是受控的货源实例表单。
  const [instanceForm, setInstanceForm] =
    useState<FulfillmentInstanceInput>(emptyInstance);
  // showInstanceForm 控制货源实例表单的渐进展开。
  const [showInstanceForm, setShowInstanceForm] = useState(false);
  // showPurchaseForm 控制手动采购表单的渐进展开。
  const [showPurchaseForm, setShowPurchaseForm] = useState(false);
  // purchaseInstanceID 是手动采购选择的货源实例。
  const [purchaseInstanceID, setPurchaseInstanceID] = useState(0);
  // purchaseGoodsID 是手动采购选择的远程商品。
  const [purchaseGoodsID, setPurchaseGoodsID] = useState(0);
  // purchaseExternalNo 是防止重复扣款的稳定外部单号。
  const [purchaseExternalNo, setPurchaseExternalNo] = useState("");
  // purchaseAttach 是直充商品所需的 JSON 附加字段。
  const [purchaseAttach, setPurchaseAttach] = useState("{}");
  // loading 表示首次数据请求正在进行。
  const [loading, setLoading] = useState(true);
  // busy 保存当前正在执行的交互标识。
  const [busy, setBusy] = useState("");
  // message 是页面内可见的成功或错误反馈。
  const [message, setMessage] = useState("");

  /** loadAll 并行读取实例和履约订单。 */
  const loadAll = async (): Promise<void> => {
    setLoading(true);
    try {
      // result 是货源实例和订单的并行请求结果。
      const result = await Promise.all([
        listFulfillmentInstances(),
        listFulfillmentOrders(),
      ]);
      setInstances(result[0]);
      setOrders(result[1]);
    } catch (error /* error 是页面初始数据请求失败原因。 */) {
      setMessage(error instanceof Error ? error.message : "货源数据加载失败");
    } finally {
      setLoading(false);
    }
  };

  useEffect(
    /* initialLoad 在页面挂载时读取货源数据。 */ () => {
      void loadAll();
    },
    [],
  );

  /** submitInstance 保存受控的货源实例表单。 */
  const submitInstance = async (event: React.FormEvent): Promise<void> => {
    event.preventDefault();
    setBusy("instance");
    setMessage("");
    try {
      await createFulfillmentInstance(instanceForm);
      setInstanceForm(emptyInstance);
      setShowInstanceForm(false);
      setMessage("货源实例已保存");
      await loadAll();
    } catch (error /* error 是实例创建失败原因。 */) {
      setMessage(error instanceof Error ? error.message : "货源实例保存失败");
    } finally {
      setBusy("");
    }
  };

  /** removeInstance 删除一个未使用的货源实例。 */
  const removeInstance = async (instanceId: number): Promise<void> => {
    if (!window.confirm("确定删除这个货源实例吗？")) return;
    setBusy(`instance-${instanceId}`);
    try {
      await deleteFulfillmentInstance(instanceId);
      await loadAll();
    } catch (error /* error 是实例删除失败原因。 */) {
      setMessage(error instanceof Error ? error.message : "删除失败");
    } finally {
      setBusy("");
    }
  };

  /** refreshOrder 查询远程站的最新订单状态。 */
  const refreshOrder = async (externalOrderNo: string): Promise<void> => {
    setBusy(`order-${externalOrderNo}`);
    try {
      await refreshFulfillmentOrder(externalOrderNo);
      await loadAll();
    } catch (error /* error 是订单刷新失败原因。 */) {
      setMessage(error instanceof Error ? error.message : "订单刷新失败");
    } finally {
      setBusy("");
    }
  };

  /** submitPurchase 提交一笔幂等手动采购，直充字段从 JSON 表单解析。 */
  const submitPurchase = async (event: React.FormEvent): Promise<void> => {
    event.preventDefault();
    setBusy("purchase");
    setMessage("");
    try {
      // parsedAttach 是用户按货源商品要求填写的直充附加字段。
      const parsedAttach = JSON.parse(purchaseAttach) as Record<string, string>;
      // order 是卡速售接受或幂等命中的采购单。
      const order = await purchaseFulfillmentOrder({
        instance_id: purchaseInstanceID,
        external_order_no: purchaseExternalNo,
        remote_goods_id: purchaseGoodsID,
        quantity: 1,
        attach: parsedAttach,
      });
      setMessage(
        `采购单 ${order.external_order_no} 已提交，当前状态：${stateLabel(order.state)}`,
      );
      setShowPurchaseForm(false);
      await loadAll();
    } catch (error /* error 是手动采购或 attach JSON 解析失败原因。 */) {
      setMessage(error instanceof Error ? error.message : "采购失败");
    } finally {
      setBusy("");
    }
  };

  if (loading)
    return (
      <div
        className="flex min-h-[24rem] items-center justify-center"
        role="status"
      >
        <LoaderCircle
          className="h-8 w-8 animate-spin text-sky-600"
          aria-label="正在加载货源管理"
        />
      </div>
    );

  return (
    <div className="space-y-7 animate-fade-in">
      <header className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
        <div>
          <div className="mb-2 flex items-center gap-2 text-sm font-bold text-sky-600">
            <Boxes className="h-4 w-4" /> 外部货源履约
          </div>
          <h2 className="text-4xl font-extrabold tracking-tight text-slate-950">
            货源管理
          </h2>
          <p className="mt-2 text-slate-500">
            支持卡速售 v2、卡易信 API 3.0 和蜜蜂汇云，规则仍按商品 ID 精确采购。
          </p>
        </div>
        <button
          type="button"
          onClick={
            /* showInstanceFormHandler 展开货源实例表单。 */ () =>
              setShowInstanceForm(true)
          }
          className="ios-btn-primary flex min-h-11 items-center justify-center gap-2 rounded-2xl px-5 py-3 font-bold"
        >
          <Plus className="h-5 w-5" /> 添加货源
        </button>
      </header>

      {message && (
        <div
          role="status"
          className="rounded-xl border border-sky-100 bg-sky-50 px-4 py-3 text-sm font-medium text-sky-800"
        >
          {message}
        </div>
      )}

      {showInstanceForm && (
        <form
          onSubmit={submitInstance}
          className="ios-card space-y-5 rounded-2xl bg-white p-6 shadow-lg"
        >
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-lg font-extrabold text-slate-900">
                新货源实例
              </h3>
              <p className="text-sm text-slate-500">
                密钥保存后只显示“已配置”，不会再返回明文。
              </p>
            </div>
            <button
              type="button"
              aria-label="关闭货源表单"
              onClick={
                /* closeInstanceFormHandler 关闭实例表单。 */ () =>
                  setShowInstanceForm(false)
              }
              className="min-h-11 min-w-11 rounded-xl p-3 hover:bg-slate-100"
            >
              <X className="h-5 w-5" />
            </button>
          </div>
          <div className="grid gap-4 md:grid-cols-2">
            <label className="space-y-1.5 text-sm font-bold text-slate-700">
              货源协议 *
              <select
                required
                value={instanceForm.provider}
                onChange={
                  /* providerChangeHandler 切换协议并设置对应的回调能力默认值。 */ (
                    event,
                  ) => {
                    // provider 是用户选择的协议标识。
                    const provider = event.target.value as FulfillmentProvider;
                    setInstanceForm({
                      ...instanceForm,
                      provider,
                      capabilities: providerCapabilities(
                        provider,
                        instanceForm.capabilities,
                      ),
                    });
                  }
                }
                className="ios-input w-full rounded-xl"
              >
                <option value="kasushou_v2">卡速售 v2（智客等兼容站）</option>
                <option value="kayixin_v3">卡易信 API 3.0</option>
                <option value="mifeng_v1">蜜蜂汇云</option>
              </select>
            </label>
            <label className="space-y-1.5 text-sm font-bold text-slate-700">
              实例名称 *
              <input
                required
                value={instanceForm.name}
                onChange={
                  /* nameChangeHandler 更新实例名称。 */ (event) =>
                    setInstanceForm({
                      ...instanceForm,
                      name: event.target.value,
                    })
                }
                className="ios-input w-full rounded-xl"
                placeholder="例如：智客主站"
              />
            </label>
            <label className="space-y-1.5 text-sm font-bold text-slate-700">
              站点地址 *
              <input
                required
                type="url"
                value={instanceForm.base_url}
                onChange={
                  /* baseURLChangeHandler 更新站点根地址。 */ (event) =>
                    setInstanceForm({
                      ...instanceForm,
                      base_url: event.target.value,
                    })
                }
                className="ios-input w-full rounded-xl"
                placeholder="https://example.com"
              />
            </label>
            <label className="space-y-1.5 text-sm font-bold text-slate-700">
              {merchantCredentialLabel(instanceForm.provider)} *
              <input
                required
                value={instanceForm.merchant_user_id}
                onChange={
                  /* merchantUserChangeHandler 更新当前协议的商户身份。 */ (
                    event,
                  ) =>
                    setInstanceForm({
                      ...instanceForm,
                      merchant_user_id: event.target.value,
                    })
                }
                className="ios-input w-full rounded-xl"
              />
            </label>
            <label className="space-y-1.5 text-sm font-bold text-slate-700">
              {secretCredentialLabel(instanceForm.provider)} *
              <input
                required
                type="password"
                autoComplete="new-password"
                value={instanceForm.api_key ?? ""}
                onChange={
                  /* apiKeyChangeHandler 更新待加密的协议密钥。 */ (event) =>
                    setInstanceForm({
                      ...instanceForm,
                      api_key: event.target.value,
                    })
                }
                className="ios-input w-full rounded-xl"
              />
            </label>
          </div>
          <div className="flex justify-end">
            <button
              disabled={busy === "instance"}
              className="ios-btn-primary min-h-11 rounded-xl px-5 py-2.5 font-bold disabled:opacity-60"
            >
              {busy === "instance" ? "正在保存…" : "保存实例"}
            </button>
          </div>
        </form>
      )}

      <section className="ios-card overflow-hidden rounded-2xl bg-white shadow-lg">
        <div className="border-b border-slate-100 p-5">
          <h3 className="flex items-center gap-2 font-extrabold text-slate-900">
            <ShieldCheck className="h-5 w-5 text-emerald-600" /> 货源实例
          </h3>
        </div>
        <div className="grid gap-4 p-5 lg:grid-cols-2">
          {instances.length === 0 && (
            <div className="col-span-full rounded-xl border border-dashed border-slate-200 p-8 text-center text-slate-500">
              还没有货源实例，先选择协议并添加一个货源站。
            </div>
          )}
          {instances.map(
            /* instanceCardMapper 渲染单个货源实例卡片。 */ (instance) => (
              <article
                key={instance.id}
                className="rounded-2xl border border-slate-200 p-5"
              >
                <div className="flex items-start justify-between gap-4">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h4 className="truncate font-extrabold text-slate-900">
                        {instance.name}
                      </h4>
                      <span
                        className={`rounded-full px-2 py-1 text-xs font-bold ${instance.enabled ? "bg-emerald-50 text-emerald-700" : "bg-slate-100 text-slate-500"}`}
                      >
                        {instance.enabled ? "已启用" : "已停用"}
                      </span>
                      <span className="rounded-full bg-sky-50 px-2 py-1 text-xs font-bold text-sky-700">
                        {providerLabel(instance.provider)}
                      </span>
                    </div>
                    <p className="mt-2 truncate text-sm text-slate-500">
                      {instance.base_url}
                    </p>
                    <div className="mt-3 flex items-center gap-2 text-xs font-bold text-slate-500">
                      <KeyRound className="h-4 w-4" />{" "}
                      {instance.has_api_key
                        ? `${secretCredentialLabel(instance.provider)} 已加密配置`
                        : `未配置 ${secretCredentialLabel(instance.provider)}`}
                    </div>
                  </div>
                  <button
                    type="button"
                    aria-label={`删除 ${instance.name}`}
                    disabled={busy === `instance-${instance.id}`}
                    onClick={
                      /* deleteInstanceHandler 删除当前货源实例。 */ () =>
                        void removeInstance(instance.id)
                    }
                    className="min-h-11 min-w-11 rounded-xl p-3 text-slate-400 hover:bg-red-50 hover:text-red-600"
                  >
                    <Trash2 className="h-5 w-5" />
                  </button>
                </div>
                <div className="mt-4 rounded-xl bg-slate-50 px-3 py-2 font-mono text-xs text-slate-500">
                  {merchantCredentialLabel(instance.provider)}:{" "}
                  {instance.merchant_user_id}
                </div>
              </article>
            ),
          )}
        </div>
      </section>

      <section className="ios-card overflow-hidden rounded-2xl bg-white shadow-lg">
        <div className="flex items-center justify-between gap-4 border-b border-slate-100 p-5">
          <div>
            <h3 className="flex items-center gap-2 font-extrabold text-slate-900">
              <PackageSearch className="h-5 w-5 text-violet-600" /> 履约订单
            </h3>
            <p className="mt-1 text-sm text-slate-500">
              超时时使用原外部单号查单，不会重复采购。
            </p>
          </div>
          <button
            type="button"
            onClick={
              /* showPurchaseFormHandler 展开手动采购表单。 */ () => {
                setPurchaseExternalNo(`manual-${Date.now()}`);
                setShowPurchaseForm(true);
              }
            }
            className="min-h-11 rounded-xl bg-violet-50 px-4 text-sm font-bold text-violet-700 hover:bg-violet-100"
          >
            <ShoppingCart className="mr-1 inline h-4 w-4" /> 手动采购
          </button>
        </div>
        {showPurchaseForm && (
          <form
            onSubmit={submitPurchase}
            className="grid gap-4 border-b border-slate-100 bg-violet-50/30 p-5 md:grid-cols-2"
          >
            <label className="space-y-1 text-sm font-bold text-slate-700">
              货源实例 *
              <select
                required
                value={purchaseInstanceID}
                onChange={
                  /* purchaseInstanceChangeHandler 选择手动采购实例。 */ (
                    event,
                  ) => setPurchaseInstanceID(Number(event.target.value))
                }
                className="ios-input w-full rounded-xl"
              >
                <option value={0}>请选择</option>
                {instances
                  .filter(
                    /* purchaseInstanceFilter 只显示启用实例。 */ (instance) =>
                      instance.enabled,
                  )
                  .map(
                    /* purchaseInstanceMapper 渲染采购实例选项。 */ (
                      instance,
                    ) => (
                      <option key={instance.id} value={instance.id}>
                        {instance.name}
                      </option>
                    ),
                  )}
              </select>
            </label>
            <label className="space-y-1 text-sm font-bold text-slate-700">
              货源商品 ID *
              <input
                required
                type="number"
                min="1"
                value={purchaseGoodsID || ""}
                onChange={
                  /* purchaseGoodsChangeHandler 直接填写手动采购商品 ID。 */ (
                    event,
                  ) => setPurchaseGoodsID(Number(event.target.value))
                }
                className="ios-input w-full rounded-xl"
                placeholder="例如：4366"
              />
            </label>
            <label className="space-y-1 text-sm font-bold text-slate-700">
              外部单号 *
              <input
                required
                value={purchaseExternalNo}
                onChange={
                  /* purchaseExternalNoChangeHandler 更新采购幂等键。 */ (
                    event,
                  ) => setPurchaseExternalNo(event.target.value)
                }
                className="ios-input w-full rounded-xl font-mono"
              />
              <span className="block text-xs font-normal text-slate-500">
                重试时必须使用同一单号。
              </span>
            </label>
            <label className="space-y-1 text-sm font-bold text-slate-700">
              直充 attach JSON
              <textarea
                rows={3}
                value={purchaseAttach}
                onChange={
                  /* purchaseAttachChangeHandler 更新直充附加字段 JSON。 */ (
                    event,
                  ) => setPurchaseAttach(event.target.value)
                }
                className="ios-input w-full rounded-xl font-mono text-xs"
                placeholder={'{"account":"13800000000"}'}
              />
            </label>
            <div className="flex gap-3 md:col-span-2">
              <button
                disabled={busy === "purchase"}
                className="ios-btn-primary min-h-11 rounded-xl px-5 font-bold"
              >
                {busy === "purchase" ? "正在提交…" : "确认采购"}
              </button>
              <button
                type="button"
                onClick={
                  /* closePurchaseFormHandler 关闭手动采购表单。 */ () =>
                    setShowPurchaseForm(false)
                }
                className="min-h-11 rounded-xl px-4 font-bold text-slate-600"
              >
                取消
              </button>
            </div>
          </form>
        )}
        <div className="overflow-x-auto">
          <table className="w-full min-w-[760px] text-left text-sm">
            <thead className="bg-slate-50 text-xs uppercase tracking-wider text-slate-500">
              <tr>
                <th className="px-5 py-4">外部单号</th>
                <th className="px-5 py-4">远程单号</th>
                <th className="px-5 py-4">商品 / 数量</th>
                <th className="px-5 py-4">状态</th>
                <th className="px-5 py-4">结果</th>
                <th className="px-5 py-4 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100">
              {orders.length === 0 && (
                <tr>
                  <td colSpan={6} className="p-8 text-center text-slate-500">
                    暂无履约订单。
                  </td>
                </tr>
              )}
              {orders.map(
                /* orderRowMapper 渲染单笔外部履约订单。 */ (order) => (
                  <tr key={order.id}>
                    <td className="px-5 py-4 font-mono text-xs text-slate-700">
                      {order.external_order_no}
                    </td>
                    <td className="px-5 py-4 font-mono text-xs text-slate-500">
                      {order.remote_order_no || "—"}
                    </td>
                    <td className="px-5 py-4">
                      #{order.remote_goods_id} × {order.quantity}
                    </td>
                    <td className="px-5 py-4">
                      <span
                        className={`rounded-full px-2.5 py-1 text-xs font-bold ${stateClass(order.state)}`}
                      >
                        {stateLabel(order.state)}
                      </span>
                    </td>
                    <td
                      className={`max-w-xs px-5 py-4 ${order.error_message ? "text-red-600" : "text-slate-600"}`}
                    >
                      {order.error_message ||
                        (order.card_list?.length
                          ? `${order.card_list.length} 条卡密`
                          : order.recharge_info || order.recharge_hints || "—")}
                    </td>
                    <td className="px-5 py-4 text-right">
                      <div className="flex justify-end gap-2">
                        {(order.error_message ||
                          order.state === "cancelled" ||
                          order.state === "refunded") && (
                          <button
                            type="button"
                            onClick={
                              /* retryAutomationHandler 前往保留完整规则参数的安全重试入口。 */ () => {
                                window.location.href = "/app/rules";
                              }
                            }
                            className="min-h-11 rounded-xl bg-amber-50 px-3 text-xs font-bold text-amber-800 hover:bg-amber-100"
                          >
                            处理重试
                          </button>
                        )}
                        <button
                          type="button"
                          disabled={busy === `order-${order.external_order_no}`}
                          onClick={
                            /* refreshOrderHandler 查询当前订单远程状态。 */ () =>
                              void refreshOrder(order.external_order_no)
                          }
                          className="min-h-11 rounded-xl px-3 text-xs font-bold text-sky-700 hover:bg-sky-50"
                        >
                          <RefreshCw
                            className={`mr-1 inline h-4 w-4 ${busy === `order-${order.external_order_no}` ? "animate-spin" : ""}`}
                          />{" "}
                          查询状态
                        </button>
                      </div>
                    </td>
                  </tr>
                ),
              )}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
};

export default Fulfillment;
