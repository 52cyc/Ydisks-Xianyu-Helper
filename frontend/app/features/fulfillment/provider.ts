/** FulfillmentProvider 是货源管理当前支持的协议标识。 */
export type FulfillmentProvider = "kasushou_v2" | "kayixin_v3";

/** FulfillmentCapabilities 是协议切换时需要保留的能力字段。 */
export interface FulfillmentCapabilities {
  /** order_list 表示供应站是否开放订单列表能力。 */
  order_list?: boolean;
  /** order_callback 表示供应站是否使用当前系统支持的订单回调。 */
  order_callback?: boolean;
  /** cancel_callback 表示供应站是否回调取消结果。 */
  cancel_callback?: boolean;
  /** cancel_request_mode 是供应站取消请求的参数模式。 */
  cancel_request_mode?: "callback_url" | "card_list" | "ordersn_only";
  /** card_show_type 表示卡密结果是否携带展示类型。 */
  card_show_type?: boolean;
}

/** providerLabel 返回货源协议的中文名称。 */
export const providerLabel = (provider: string): string =>
  provider === "kayixin_v3" ? "卡易信 API 3.0" : "卡速售 v2";

/** merchantCredentialLabel 返回当前协议的商户身份字段名称。 */
export const merchantCredentialLabel = (provider: string): string =>
  provider === "kayixin_v3" ? "APP ID" : "UserId";

/** secretCredentialLabel 返回当前协议的密钥字段名称。 */
export const secretCredentialLabel = (provider: string): string =>
  provider === "kayixin_v3" ? "AppSecret" : "API Key";

/** providerCapabilities 保留通用能力并设置协议当前支持的回调默认值。 */
export const providerCapabilities = (
  provider: FulfillmentProvider,
  current?: FulfillmentCapabilities,
): Required<FulfillmentCapabilities> => ({
  order_list: current?.order_list ?? false,
  order_callback: provider === "kasushou_v2",
  cancel_callback: current?.cancel_callback ?? false,
  cancel_request_mode: current?.cancel_request_mode ?? "callback_url",
  card_show_type: current?.card_show_type ?? false,
});
