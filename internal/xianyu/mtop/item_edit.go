// Package mtop: 商品编辑域 — 读取 PC 编辑详情后仅替换售价，避免覆盖标题、图片和类目。
package mtop

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	ItemEditDetailAPI = "https://h5api.m.goofish.com/h5/mtop.idle.pc.idleitem.editDetail/1.0/"
	ItemEditAPI       = "https://h5api.m.goofish.com/h5/mtop.idle.pc.idleitem.edit/1.0/"
)

// UpdateItemPriceContext 修改普通商品的商品页售价。多规格商品需要逐 SKU 映射，当前明确拒绝以避免误改。
func (c *ClientImpl) UpdateItemPriceContext(ctx context.Context, cookiesStr, itemID string, priceCents int64) (bool, []string, string, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" || priceCents <= 0 || priceCents > 100000000 {
		return false, nil, cookiesStr, errors.New("商品 ID 或目标售价无效")
	}
	currentCookies := cookiesStr
	if session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	var lastRet []string
	for attempt := 0; attempt < 4; attempt++ {
		previousCookies := currentCookies
		ok, ret, updated, requestErr := c.updateItemPriceOnce(ctx, currentCookies, itemID, priceCents)
		if requestErr != nil {
			return false, ret, updated, requestErr
		}
		lastRet = ret
		if updated != "" {
			currentCookies = updated
		}
		if ok {
			return true, ret, currentCookies, nil
		}
		if isSessionExpiredRet(ret) {
			return false, ret, currentCookies, sessionExpiredError("商品改价接口", ret)
		}
		if !isTokenExpiredRet(ret) || attempt == 3 {
			return false, ret, currentCookies, nil
		}
		if currentCookies == previousCookies {
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return false, ret, currentCookies, fmt.Errorf("商品改价 token 过期且刷新失败: %w", refreshErr)
			}
			if refreshed.UpdatedCookies != "" {
				currentCookies = refreshed.UpdatedCookies
			}
		}
		if waitErr := sleepCtx(ctx, MTopRetryGap); waitErr != nil {
			return false, ret, currentCookies, waitErr
		}
	}
	return false, lastRet, currentCookies, nil
}

func (c *ClientImpl) updateItemPriceOnce(ctx context.Context, cookiesStr, itemID string, priceCents int64) (bool, []string, string, error) {
	detailURL := c.ItemEditDetailURL
	if detailURL == "" {
		detailURL = ItemEditDetailAPI
	}
	detail, updated, detailErr := c.callMTop(ctx, cookiesStr, detailURL, "mtop.idle.pc.idleitem.editDetail", "1.0",
		"a21ybx.publish.0.0", "a21ybx.publish.0.0", "xianyu_item_edit_detail", map[string]any{"itemId": itemID})
	if detailErr != nil {
		return false, nil, updated, detailErr
	}
	detailRet := retFromDecoded(detail)
	if !hasMTopSuccess(detailRet) {
		return false, detailRet, updated, nil
	}
	detailData := mapFromAny(detail["data"])
	if len(detailData) == 0 {
		return false, detailRet, updated, errors.New("商品编辑详情为空")
	}
	if skuList, ok := detailData["itemSkuList"].([]any); ok && len(skuList) > 0 {
		return false, detailRet, updated, errors.New("多规格商品暂不支持同步商品页规格价格")
	}
	payload := itemEditPayload(detailData)
	payload["itemId"] = itemID
	payload["sourceId"] = firstNonEmptyString(mtopString(payload["sourceId"]), itemID)
	payload["uniqueCode"] = strconv.FormatInt(time.Now().UnixMicro(), 10)
	payload["bizcode"] = firstNonEmptyString(mtopString(payload["bizcode"]), "pcMainPublish")
	payload["publishScene"] = firstNonEmptyString(mtopString(payload["publishScene"]), "pcMainPublish")
	priceDTO := mapFromAny(payload["itemPriceDTO"])
	if priceDTO == nil {
		priceDTO = map[string]any{}
	}
	priceDTO["priceInCent"] = strconv.FormatInt(priceCents, 10)
	payload["itemPriceDTO"] = priceDTO

	editURL := c.ItemEditURL
	if editURL == "" {
		editURL = ItemEditAPI
	}
	decoded, editUpdated, editErr := c.callMTop(ctx, updated, editURL, "mtop.idle.pc.idleitem.edit", "1.0",
		"a21ybx.publish.0.0", "a21ybx.publish.0.0", "xianyu_item_edit", payload)
	if editErr != nil {
		return false, nil, editUpdated, editErr
	}
	ret := retFromDecoded(decoded)
	return hasMTopSuccess(ret), ret, editUpdated, nil
}

// itemEditPayload 只复制 PC 编辑提交接受的字段，过滤 editDetail 的跳转地址和提示等只读响应字段。
func itemEditPayload(detail map[string]any) map[string]any {
	payload := map[string]any{}
	keys := []string{
		"attribute_biz_line", "bizcode", "bucketId", "canBargain", "defaultPrice", "freebies", "itemStatus",
		"itemTypeStr", "quantity", "scene", "simpleItem", "supportBargainPrice", "topics", "asyncSecurityInfo",
		"baseParams", "itemAddrDTO", "itemCatDTO", "itemGroupDTO", "itemPostFeeDTO", "itemPriceDTO", "itemTextDTO",
		"itemTopicParams", "yhbItemInfoDTO", "imageInfoDOList", "itemLabelExtList", "itemProperties", "itemSkuList",
		"userRightsProtocols",
	}
	for _, key := range keys {
		if value, exists := detail[key]; exists {
			payload[key] = value
		}
	}
	return payload
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
