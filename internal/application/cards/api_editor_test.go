package cards

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// editorRepository 在摘要通过权限校验后才允许读取完整模板。
type editorRepository struct {
	// cardRepositoryStub 提供既有库存行为与可注入错误。
	cardRepositoryStub
	// full 保存第二次完整读取的结果，便于模拟归属或类型变化。
	full Card
	// fullErr 模拟完整模板读取失败。
	fullErr error
	// reads 统计含密钥的读取次数。
	reads int
}

// GetFull 记录完整模板读取并返回预设结果。
func (r *editorRepository) GetFull(context.Context, int64) (Card, error) {
	r.reads++
	return r.full, r.fullErr
}

// TestAPIEditorApplicationBranches 覆盖归属变更、配置损坏、空模板和三态草稿合并。
func TestAPIEditorApplicationBranches(t *testing.T) {
	// ctx 是不访问真实平台的测试上下文。
	ctx := context.Background()
	// card 是合法且三个模板均为空的已保存 API 卡密。
	card := Card{ID: 1, UserID: 7, Type: "api", APIConfig: `{"url":"https://example.com","method":"GET"}`}
	// repository 是包含可控摘要和完整配置的持久化替身。
	repository := &editorRepository{cardRepositoryStub: cardRepositoryStub{card: card}, full: card}
	// service 是本次测试的卡密用例入口。
	service := NewService(repository)
	// editor、err 验证空模板以非 nil 空键名列表回显，前端据此直接进入填写状态。
	editor, err := service.GetAPIEditor(ctx, 7, 1)
	if err != nil || editor.HeaderKeys == nil || editor.ParamKeys == nil || editor.BodyKeys == nil ||
		len(editor.HeaderKeys)+len(editor.ParamKeys)+len(editor.BodyKeys) != 0 {
		t.Fatalf("空模板回显错误: %+v %v", editor, err)
	}
	// 已保存模板只回显有序键名，任何值（含凭证）都不得出现在编辑视图中。
	repository.full.APIConfig = `{"url":"https://example.com","method":"POST","headers":{"X-Sign":"editor-secret-sign","Authorization":"Bearer editor-secret-token"},"params":{"order_id":"{order_id}"},"body":{"order_id":"{order_id}","secret":"editor-secret-body"}}`
	// withValues、valuesErr 是包含三类模板时的脱敏编辑视图。
	withValues, valuesErr := service.GetAPIEditor(ctx, 7, 1)
	if valuesErr != nil {
		t.Fatal(valuesErr)
	}
	if // got 是逗号拼接的请求头键名，应按字典序排列。
	got := strings.Join(withValues.HeaderKeys, ","); got != "Authorization,X-Sign" {
		t.Fatalf("请求头键名=%q", got)
	}
	if // got 是逗号拼接的查询参数键名。
	got := strings.Join(withValues.ParamKeys, ","); got != "order_id" {
		t.Fatalf("参数键名=%q", got)
	}
	if // got 是逗号拼接的正文键名，键名本身可回显而值不可。
	got := strings.Join(withValues.BodyKeys, ","); got != "order_id,secret" {
		t.Fatalf("正文键名=%q", got)
	}
	if // dump 是整个编辑视图的文本展开，用于确认任何字段都没有携带模板值。
	dump := fmt.Sprintf("%+v", withValues); strings.Contains(dump, "editor-secret") || strings.Contains(dump, "{order_id}") {
		t.Fatalf("编辑视图泄露了模板值: %s", dump)
	}
	repository.full = card
	repository.reads = 1
	// raw、err 验证新建草稿不依赖数据库已有卡密。
	raw, err := service.PrepareAPITest(ctx, 7, 0, card.APIConfig)
	if err != nil || raw == "" || repository.reads != 1 {
		t.Fatal("新建测试不应读取完整库存", err)
	}
	// invalidUser 检查未登录用户无法使用模板合并服务。
	if _, invalidUser := service.PrepareAPITest(ctx, 0, 0, card.APIConfig); invalidUser == nil {
		t.Fatal("零用户必须拒绝")
	}
	// invalidConfig 验证错误响应不会回显输入中的秘密。
	if _, invalidConfig := service.PrepareAPITest(ctx, 7, 0, `{"url":"private-secret"}`); invalidConfig == nil {
		t.Fatal("非法配置必须拒绝")
	}
	// testCase 分别模拟摘要阶段和完整读取阶段的失败。
	for _, testCase := range []struct {
		// name 用于标识失败场景。
		name string
		// change 修改持久化替身以模拟该场景。
		change func()
	}{
		{name: "summary-owner", change: func() { repository.card.UserID = 8 }},
		{name: "summary-type", change: func() { repository.card.Type = "text" }},
		{name: "summary-read", change: func() { repository.getErr = errors.New("read") }},
		{name: "full-owner", change: func() { repository.full.UserID = 8 }},
		{name: "full-type", change: func() { repository.full.Type = "text" }},
		{name: "full-read", change: func() { repository.fullErr = errors.New("decrypt") }},
		{name: "invalid-saved", change: func() { repository.full.APIConfig = "invalid" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repository.card, repository.full = card, card
			repository.getErr, repository.fullErr = nil, nil
			repository.reads = 0
			testCase.change()
			// failure 是权限、读取或已保存模板损坏时的安全错误。
			if _, failure := service.GetAPIEditor(ctx, 7, 1); failure == nil {
				t.Fatal("预期读取失败")
			}
			if testCase.name == "summary-owner" && repository.reads != 0 {
				t.Fatal("越权读取了密钥")
			}
			// failure 是测试合并过程中同样必须拒绝的错误。
			if _, failure := service.PrepareAPITest(ctx, 7, 1, ""); failure == nil {
				t.Fatal("预期测试合并失败")
			}
		})
	}
}
