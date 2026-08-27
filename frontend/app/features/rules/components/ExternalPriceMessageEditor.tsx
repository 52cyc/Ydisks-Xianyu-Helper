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

// defaultSafePriceFailureNotice 是保护价拦截后携带最新订单总价的重新下单提示。
const defaultSafePriceFailureNotice =
  "您好，货源价格已发生变化，本次订单暂时无法按原价发货。当前最新价格为 ¥{price}。请先申请退款；如仍需要，请重新拍下但不要付款，系统会按最新价格为您改价。";

// legacyFailureNotice 是升级前的默认异常提示，用于编辑时显示新版简化文案。
const legacyFailureNotice =
  "您好，您的订单正在人工核实处理中，目前暂时无法自动发货。请先不要重复下单，我们会尽快处理；如不愿等待，也可以申请退款。";

// defaultFailureNotice 是货源站异常且全部自动重试失败后的默认人工处理提示。
const defaultFailureNotice =
  "您好，您的订单正在人工核实处理中，目前暂时无法自动发货。请先不要重复下单，我们会尽快处理。";

// failureNoticeText 返回自定义异常文案，并把升级前默认话术更新为新版文案。
const failureNoticeText = (config: Record<string, unknown>): string => {
  // configured 是规则中已经保存的通用货源异常提示。
  const configured = String(
    config.fulfillment_failure_notice_text || "",
  ).trim();
  return !configured || configured === legacyFailureNotice
    ? defaultFailureNotice
    : configured;
};

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

// MessageTextareaProps 描述规则话术输入框的统一标签、行数、帮助文本和值变更边界。
interface MessageTextareaProps {
  /** label 是输入框上方的业务名称。 */
  label: string;
  /** rows 控制输入框默认可见行数。 */
  rows: number;
  /** value 是当前规则保存或默认填充的话术。 */
  value: string;
  /** help 展示该话术允许使用的变量说明。 */
  help: React.ReactNode;
  /** onChange 把编辑后的完整话术回写到规则配置。 */
  onChange: (value: string) => void;
}

/** MessageTextarea 统一渲染消息文案输入框，减少规则页重复结构。 */
const MessageTextarea: React.FC<MessageTextareaProps> = ({
  label,
  rows,
  value,
  help,
  onChange,
}) => (
  <label className="block text-xs font-bold text-gray-600">
    {label}
    <textarea
      rows={rows}
      maxLength={1000}
      value={value}
      onChange={
        /* messageTextareaChangeHandler 把当前话术值交给对应规则字段。 */ (
          event,
        ) => onChange(event.target.value)
      }
      className="mt-2 w-full ios-input rounded-xl px-3 py-2.5 text-sm leading-6"
    />
    <span className="mt-1 block text-[11px] font-normal text-gray-500">
      {help}
    </span>
  </label>
);

// MessageToggleProps 描述消息功能开关的标题、说明、状态和禁用条件。
interface MessageToggleProps {
  /** label 是消息功能开关名称。 */
  label: string;
  /** detail 解释消息触发时机和限制。 */
  detail: string;
  /** checked 表示当前消息功能是否开启。 */
  checked: boolean;
  /** disabled 表示当前规则尚不具备启用条件。 */
  disabled?: boolean;
  /** onChange 把新开关状态回写到规则配置。 */
  onChange: (checked: boolean) => void;
}

/** MessageToggle 统一渲染规则消息开关。 */
const MessageToggle: React.FC<MessageToggleProps> = ({
  label,
  detail,
  checked,
  disabled,
  onChange,
}) => (
  <label className="flex cursor-pointer items-center justify-between gap-3 text-sm font-bold text-gray-800">
    <span>
      {label}
      <span className="mt-1 block text-xs font-normal text-gray-500">
        {detail}
      </span>
    </span>
    <input
      type="checkbox"
      disabled={disabled}
      checked={checked}
      onChange={
        /* messageToggleChangeHandler 把布尔状态交给对应消息配置。 */ (
          event,
        ) => onChange(event.target.checked)
      }
      className="h-4 w-4 accent-violet-600 disabled:opacity-40"
    />
  </label>
);

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
    <MessageToggle
      label="首次咨询自动查询报价"
      detail="同一聊天会话只成功报价一次；多规格商品会按规格发送价格列表。"
      checked={config.price_guidance_enabled === true}
      onChange={
        /* priceGuidanceToggleHandler 切换该商品首次咨询引导。 */ (
          checked,
        ) =>
          onChange({
            price_guidance_enabled: checked,
            price_query_prompt_text: String(
              config.price_query_prompt_text || defaultQueryPrompt,
            ),
            price_guidance_text: guidanceText(config),
          })
      }
    />
    {config.price_guidance_enabled === true && (
      <div className="space-y-4">
        <MessageTextarea
          label="查询前提示文案"
          rows={2}
          value={String(config.price_query_prompt_text || defaultQueryPrompt)}
          help={<>支持 {"{item_title}"}、{"{item_id}"}。</>}
          onChange={
            /* priceQueryPromptTextHandler 更新货源查询开始前的等待提示。 */ (
              value,
            ) => onChange({ price_query_prompt_text: value })
          }
        />
        <MessageTextarea
          label="报价结果与下单引导"
          rows={5}
          value={guidanceText(config)}
          help={<>{"{price_list}"} 显示报价；另支持 {"{price}"}、{"{item_title}"}、{"{item_id}"}。</>}
          onChange={
            /* priceGuidanceTextHandler 更新查询成功后的报价和拍下未付款话术。 */ (
              value,
            ) => onChange({ price_guidance_text: value })
          }
        />
      </div>
    )}
    <MessageToggle
      label="改价成功后发送最新价格"
      detail="只有闲鱼订单明确改价成功后才发送，失败时不会催买家付款。"
      checked={config.price_adjusted_notice_enabled === true}
      onChange={
        /* priceAdjustedNoticeToggleHandler 切换最终价格通知。 */ (
          checked,
        ) =>
          onChange({
            price_adjusted_notice_enabled: checked,
            price_adjusted_notice_text: String(
              config.price_adjusted_notice_text || defaultAdjustedNotice,
            ),
          })
      }
    />
    {config.price_adjusted_notice_enabled === true && (
      <MessageTextarea
        label="改价成功通知文案"
        rows={3}
        value={String(
          config.price_adjusted_notice_text || defaultAdjustedNotice,
        )}
        help={<>支持 {"{price}"}、{"{quantity}"}、{"{item_title}"}、{"{order_id}"}。</>}
        onChange={
          /* priceAdjustedNoticeTextHandler 更新包含最终价格的付款提示。 */ (
            value,
          ) => onChange({ price_adjusted_notice_text: value })
        }
      />
    )}
    <div className="border-t border-violet-200 pt-4 space-y-4">
      <MessageToggle
        label="采购最终失败后通知买家"
        detail="重试耗尽后每个订单最多发送一次；订单保持待发货并继续通知管理员。"
        disabled={!hasExternalFulfillment}
        checked={config.fulfillment_failure_notice_enabled === true}
        onChange={
          /* fulfillmentFailureNoticeToggleHandler 切换外部采购最终失败后的买家提示。 */ (
            checked,
          ) =>
            onChange({
              fulfillment_failure_notice_enabled: checked,
              fulfillment_safe_price_notice_text: String(
                config.fulfillment_safe_price_notice_text ||
                  defaultSafePriceFailureNotice,
              ),
              fulfillment_failure_notice_text: failureNoticeText(config),
            })
        }
      />
      {config.fulfillment_failure_notice_enabled === true && (
        <div className="space-y-4">
          <MessageTextarea
            label="保护价拦截提示"
            rows={4}
            value={String(config.fulfillment_safe_price_notice_text || defaultSafePriceFailureNotice)}
            help={<>{"{price}"} 是按最新货源价和固定加价计算的订单总价。</>}
            onChange={
              /* safePriceFailureNoticeTextHandler 更新涨价后的最新报价和重新下单引导。 */ (
                value,
              ) => onChange({ fulfillment_safe_price_notice_text: value })
            }
          />
          <MessageTextarea
            label="货源站异常提示"
            rows={3}
            value={failureNoticeText(config)}
            help={<>支持 {"{item_title}"}、{"{order_id}"}、{"{quantity}"}，请勿填写成本或利润。</>}
            onChange={
              /* fulfillmentFailureNoticeTextHandler 更新货源站异常时发送的人工处理话术。 */ (
                value,
              ) => onChange({ fulfillment_failure_notice_text: value })
            }
          />
        </div>
      )}
    </div>
  </div>
);

export default ExternalPriceMessageEditor;
