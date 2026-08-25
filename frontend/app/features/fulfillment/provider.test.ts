import { describe, expect, it } from "vitest";
import {
  merchantCredentialLabel,
  providerCapabilities,
  providerLabel,
  secretCredentialLabel,
} from "./provider";

describe("fulfillment provider metadata", /* providerMetadataSuite 验证协议展示和能力默认值。 */ () => {
  it("展示卡速售和卡易信各自的凭证名称", /* credentialLabelsCase 验证协议凭证标签。 */ () => {
    expect(providerLabel("kasushou_v2")).toBe("卡速售 v2");
    expect(providerLabel("kayixin_v3")).toBe("卡易信 API 3.0");
    expect(merchantCredentialLabel("kayixin_v3")).toBe("APP ID");
    expect(secretCredentialLabel("kayixin_v3")).toBe("AppSecret");
  });

  it("卡易信关闭当前无法验签的订单回调并保留其他能力", /* kayixinCapabilitiesCase 验证卡易信安全能力默认值。 */ () => {
    // capabilities 是切换到卡易信后的完整能力配置。
    const capabilities = providerCapabilities("kayixin_v3", {
      order_list: true,
      order_callback: true,
      card_show_type: true,
    });
    expect(capabilities).toEqual({
      order_list: true,
      order_callback: false,
      cancel_callback: false,
      cancel_request_mode: "callback_url",
      card_show_type: true,
    });
  });
});
