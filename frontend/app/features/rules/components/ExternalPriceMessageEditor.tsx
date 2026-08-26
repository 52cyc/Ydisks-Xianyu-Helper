import React from "react";

// legacyDefaultGuidance 是升级前的默认引导，用于在编辑时自动显示新的先报价流程。
const legacyDefaultGuidance =
  "亲，您好～本商品会根据货源最新价格自动报价。请先拍下但不要付款，系统改价完成后会通知您，确认金额后再付款。";

// defaultQueryPrompt 是系统读取货源实时报价前发送的等待提示。
const defaultQueryPrompt = "亲，您好～正在为您查询当前最新价格，请稍候。";

// defaultGuidance 是实时查询成功后发送的报价和拍下未付款引导。
const defaultGuidance =
  "当前最新报价：\n{price_list}\n如果需要，请先拍下但不要付款。系统会按您下单时的最新价格自动改价，确认金额后再付款。";

// defaultAdjustedNotice 是实时改价成功后携带最终订单价的默认付款提示。
const defaultAdjustedNotice =
  "已按最新价格为您改价为 ¥{price}，请核对订单金额，确认无误后再付款。";

// defaultFailureNotice 是外部采购全部自动重试失败后发送的默认人工处理提示。
const defaultFailureNotice =
  "您好，您的订单正在人工核实处理中，目前暂时无法自动发货。请先不要重复下单，我们会尽快处理；如不愿等待，也可以申请退款。";

// guidanceText 返回当前自定义报价文案，并把旧版默认话术无感升级为新版默认值。
const guidanceText = (config: Record<string, unknown>): string => {
  // configured 是规则已经保存的咨询引导文本。
  const configured = String(config.price_guidance_text || "").trim();
  return !configured || configured === legacyDefaultGuidance
    ? defaultGuidance
    : configured;
};

// ExternalPriceMessageEditorProps 描述商品级咨询引导与改价通知编辑器的受控输入。
interface ExternalPriceMessageEditorProps {
  /** config 是规则 JSON 中已解析的消息配置，不包含货源凭证。 */
  config: Record<string, unknown>;
  /** hasPendingPrice 表示至少一条外部发货内容已开启实时跟价。 */
  hasPendingPrice: boolean;
  /** hasExternalFulfillment 表示规则包含可配置最终失败通知的外部货源内容。 */
  hasExternalFulfillment: boolean;
  /** onChange 把局部消息配置交给父表单合并，避免覆盖其他规则字段。 */
  onChange: (patch: Record<string, boolean | string>) => void;
}

/** ExternalPriceMessageEditor 编辑按商品生效的实时询价、改价成功和采购最终失败通知。 */
const ExternalPriceMessageEditor: React.FC<ExternalPriceMessageEditorProps> = ({
  config,
  hasExternalFulfillment,
  hasPendingPrice,
  onChange,
}) => (
  <div className="rounded-2xl border border-violet-200 bg-violet-50/60 p-4 space-y-4">
    <div>
      <h5 className="text-sm font-black text-violet-900">
        买家询价与履约通知
      </h5>
      <p className="mt-1 text-xs leading-5 text-violet-700">
        按本商品规则单独生效。询价与改价消息服务拍下前流程；采购最终失败提示只在全部自动重试结束后发送，不会泄露成本或保护价。
      </p>
      {!hasPendingPrice &&
        (config.price_guidance_enabled === true ||
          config.price_adjusted_notice_enabled === true) && (
        <p className="mt-2 text-xs font-bold text-amber-700">
          当前没有发货内容开启待付款自动改价，请关闭询价和改价通知，或重新开启动态改价。
        </p>
      )}
    </div>
    <label className="flex cursor-pointer items-center justify-between gap-3 text-sm font-bold text-gray-800">
      <span>
        首次咨询自动查询报价
        <span className="mt-1 block text-xs font-normal text-gray-500">
          同一聊天会话只成功报价一次；多规格商品会按规格发送价格列表。
        </span>
      </span>
      <input
        type="checkbox"
        checked={config.price_guidance_enabled === true}
        onChange={
          /* priceGuidanceToggleHandler 切换该商品首次咨询引导。 */ (
            event,
          ) =>
            onChange({
              price_guidance_enabled: event.target.checked,
              price_query_prompt_text: String(
                config.price_query_prompt_text || defaultQueryPrompt,
              ),
              price_guidance_text: guidanceText(config),
            })
        }
        className="h-4 w-4 accent-violet-600"
      />
    </label>
    {config.price_guidance_enabled === true && (
      <div className="space-y-4">
        <label className="block text-xs font-bold text-gray-600">
          查询前提示文案
          <textarea
            rows={2}
            maxLength={1000}
            value={String(
              config.price_query_prompt_text || defaultQueryPrompt,
            )}
            onChange={
              /* priceQueryPromptTextHandler 更新货源查询开始前的等待提示。 */ (
                event,
              ) => onChange({ price_query_prompt_text: event.target.value })
            }
            className="mt-2 w-full ios-input rounded-xl px-3 py-2.5 text-sm leading-6"
          />
          <span className="mt-1 block text-[11px] font-normal text-gray-500">
            支持 {"{item_title}"}、{"{item_id}"}。
          </span>
        </label>
        <label className="block text-xs font-bold text-gray-600">
          报价结果与下单引导
          <textarea
            rows={5}
            maxLength={1000}
            value={guidanceText(config)}
            onChange={
              /* priceGuidanceTextHandler 更新查询成功后的报价和拍下未付款话术。 */ (
                event,
              ) => onChange({ price_guidance_text: event.target.value })
            }
            className="mt-2 w-full ios-input rounded-xl px-3 py-2.5 text-sm leading-6"
          />
          <span className="mt-1 block text-[11px] font-normal text-gray-500">
            {"{price_list}"} 会显示单价或多规格价格列表；单规格还可使用
            {" {price}"}。同时支持 {"{item_title}"}、{"{item_id}"}。
          </span>
        </label>
      </div>
    )}
    <label className="flex cursor-pointer items-center justify-between gap-3 text-sm font-bold text-gray-800">
      <span>
        改价成功后发送最新价格
        <span className="mt-1 block text-xs font-normal text-gray-500">
          只有闲鱼订单明确改价成功后才发送，失败时不会催买家付款。
        </span>
      </span>
      <input
        type="checkbox"
        checked={config.price_adjusted_notice_enabled === true}
        onChange={
          /* priceAdjustedNoticeToggleHandler 切换最终价格通知。 */ (
            event,
          ) =>
            onChange({
              price_adjusted_notice_enabled: event.target.checked,
              price_adjusted_notice_text: String(
                config.price_adjusted_notice_text || defaultAdjustedNotice,
              ),
            })
        }
        className="h-4 w-4 accent-violet-600"
      />
    </label>
    {config.price_adjusted_notice_enabled === true && (
      <label className="block text-xs font-bold text-gray-600">
        改价成功通知文案
        <textarea
          rows={3}
          maxLength={1000}
          value={String(
            config.price_adjusted_notice_text || defaultAdjustedNotice,
          )}
          onChange={
            /* priceAdjustedNoticeTextHandler 更新包含最终价格的付款提示。 */ (
              event,
            ) => onChange({ price_adjusted_notice_text: event.target.value })
          }
          className="mt-2 w-full ios-input rounded-xl px-3 py-2.5 text-sm leading-6"
        />
        <span className="mt-1 block text-[11px] font-normal text-gray-500">
          支持 {"{price}"}、{"{quantity}"}、{"{item_title}"}、
          {"{order_id}"}。
        </span>
      </label>
    )}
    <div className="border-t border-violet-200 pt-4 space-y-4">
      <label className="flex cursor-pointer items-center justify-between gap-3 text-sm font-bold text-gray-800">
        <span>
          采购最终失败后通知买家
          <span className="mt-1 block text-xs font-normal leading-5 text-gray-500">
            首次失败不会打扰买家；全部自动重试结束后每个订单最多发送一次，订单仍保持待发货并继续通知管理员。
          </span>
        </span>
        <input
          type="checkbox"
          disabled={!hasExternalFulfillment}
          checked={config.fulfillment_failure_notice_enabled === true}
          onChange={
            /* fulfillmentFailureNoticeToggleHandler 切换外部采购最终失败后的买家提示。 */ (
              event,
            ) =>
              onChange({
                fulfillment_failure_notice_enabled: event.target.checked,
                fulfillment_failure_notice_text: String(
                  config.fulfillment_failure_notice_text ||
                    defaultFailureNotice,
                ),
              })
          }
          className="h-4 w-4 accent-amber-600 disabled:opacity-40"
        />
      </label>
      {config.fulfillment_failure_notice_enabled === true && (
        <label className="block text-xs font-bold text-gray-600">
          最终失败提示文案
          <textarea
            rows={4}
            maxLength={1000}
            value={String(
              config.fulfillment_failure_notice_text || defaultFailureNotice,
            )}
            onChange={
              /* fulfillmentFailureNoticeTextHandler 更新重试耗尽后发送给买家的人工处理话术。 */ (
                event,
              ) =>
                onChange({
                  fulfillment_failure_notice_text: event.target.value,
                })
            }
            className="mt-2 w-full ios-input rounded-xl px-3 py-2.5 text-sm leading-6"
          />
          <span className="mt-1 block text-[11px] font-normal leading-5 text-gray-500">
            支持 {"{item_title}"}、{"{item_id}"}、{"{order_id}"}、
            {"{quantity}"}。请勿填写采购成本、保护价或利润信息。
          </span>
        </label>
      )}
    </div>
  </div>
);

export default ExternalPriceMessageEditor;
