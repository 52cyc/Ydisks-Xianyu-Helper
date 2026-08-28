package mtop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestUpdateItemPricePreservesEditDetailPayload(t *testing.T) {
	var editPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("api") {
		case "mtop.idle.pc.idleitem.editDetail":
			_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"itemId":"item-1","simpleItem":"true","itemTextDTO":{"title":"原标题","desc":"原描述"},"itemPriceDTO":{"priceInCent":"950","origPriceInCent":"1200"},"imageInfoDOList":[{"url":"https://img.example/a.jpg"}]}}`))
		case "mtop.idle.pc.idleitem.edit":
			body, _ := io.ReadAll(r.Body)
			form, _ := url.ParseQuery(string(body))
			_ = json.Unmarshal([]byte(form.Get("data")), &editPayload)
			_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"success":true}}`))
		default:
			http.Error(w, "unexpected api", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := &ClientImpl{HTTPClient: server.Client(), ItemEditDetailURL: server.URL, ItemEditURL: server.URL}
	ok, _, _, err := client.UpdateItemPriceContext(context.Background(), "unb=1; _m_h5_tk=token_1", "item-1", 969)
	if err != nil || !ok {
		t.Fatalf("商品改价失败: ok=%v err=%v", ok, err)
	}
	priceDTO := mapFromAny(editPayload["itemPriceDTO"])
	textDTO := mapFromAny(editPayload["itemTextDTO"])
	if mtopString(priceDTO["priceInCent"]) != "969" || mtopString(priceDTO["origPriceInCent"]) != "1200" {
		t.Fatalf("价格字段未最小替换: %+v", priceDTO)
	}
	if mtopString(textDTO["title"]) != "原标题" || len(editPayload["imageInfoDOList"].([]any)) != 1 {
		t.Fatalf("商品原字段被覆盖: %+v", editPayload)
	}
	if strings.TrimSpace(mtopString(editPayload["uniqueCode"])) == "" || mtopString(editPayload["itemId"]) != "item-1" {
		t.Fatalf("编辑定位字段缺失: %+v", editPayload)
	}
}
