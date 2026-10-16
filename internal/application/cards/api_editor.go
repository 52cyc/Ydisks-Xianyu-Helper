package cards

import (
	"context"
	"sort"
)

// APIEditor 是所属用户打开编辑弹窗时的脱敏配置视图：只包含非敏感字段和模板顶层键名，
// 请求头、查询参数和正文的值一律留在服务端，编辑与测试通过“保留”语义由服务端合并。
type APIEditor struct {
	// URL 是请求目标地址。
	URL string
	// Method 是请求方法。
	Method string
	// TimeoutSeconds 是请求超时秒数。
	TimeoutSeconds int
	// ContentType 是正文编码类型。
	ContentType string
	// HeaderKeys、ParamKeys、BodyKeys 是已保存模板按字典序排列的顶层键名，不含任何值；空切片表示未配置。
	HeaderKeys, ParamKeys, BodyKeys []string
	// ResponsePath 是返回值提取路径。
	ResponsePath string
	// RetryEnabled 表示是否启用幂等重试。
	RetryEnabled bool
}

// ownedAPIConfig 先核对 userID 对 cardID 的所有权，再读取当前 API 模板；ctx 控制读取取消。
func (s *Service) ownedAPIConfig(ctx context.Context, userID, cardID int64) (string, error) {
	// summary、err 是不向调用方暴露秘密的归属查询结果。
	summary, err := s.Get(ctx, userID, cardID)
	if err != nil {
		return "", err
	}
	if summary.Type != "api" {
		return "", &ValidationError{Message: "只有 API 卡密支持此操作"}
	}
	// card、err 保存通过归属校验后的完整配置，并再次确认读取期间所有者没有变化。
	card, err := s.ownedCardFull(ctx, userID, cardID)
	if err != nil {
		return "", err
	}
	if card.Type != "api" {
		return "", &ValidationError{Message: "卡密类型已变化，请重新打开"}
	}
	return card.APIConfig, nil
}

// GetAPIEditor 为所属用户读取 cardID 的脱敏编辑视图；模板值不离开服务端，普通详情仍只返回摘要。
func (s *Service) GetAPIEditor(ctx context.Context, userID, cardID int64) (APIEditor, error) {
	// raw、err 保存权限核验后的模板及查询错误。
	raw, err := s.ownedAPIConfig(ctx, userID, cardID)
	if err != nil {
		return APIEditor{}, err
	}
	// config、err 保存经过校验的配置，错误不回显模板内容。
	config, err := ParseAPIConfig(raw)
	if err != nil {
		return APIEditor{}, &ValidationError{Message: "已保存的 API 配置无效"}
	}
	return APIEditor{URL: config.URL, Method: config.Method, TimeoutSeconds: config.Timeout, ContentType: config.ContentType,
		HeaderKeys: apiTemplateKeys(config.Headers), ParamKeys: apiTemplateKeys(config.Params), BodyKeys: apiTemplateKeys(config.Body),
		ResponsePath: config.ResponsePath, RetryEnabled: config.RetryEnabled}, nil
}

// apiTemplateKeys 返回模板顶层键名的有序副本，丢弃全部值；nil 或空模板返回非 nil 空切片，便于 JSON 输出 []。
func apiTemplateKeys(template map[string]any) []string {
	// keys 只保存键名，长度等于模板顶层字段数。
	keys := make([]string, 0, len(template))
	// key 是当前模板字段名，对应的值可能是密钥，因此不读取。
	for key := range template {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// PrepareAPITest 合并 userID 所有的 cardID 已保存配置和 raw 草稿，不写数据库；零标识表示新增测试。
func (s *Service) PrepareAPITest(ctx context.Context, userID, cardID int64, raw string) (string, error) {
	// err 是当前调用者或应用装配的校验错误。
	if err := s.validateUser(userID); err != nil {
		return "", err
	}
	// existing 仅在已保存卡密测试时读取，不传到浏览器。
	var existing string
	if cardID != 0 {
		// err 是归属校验或模板读取错误，越权时不会调用远端。
		var err error
		existing, err = s.ownedAPIConfig(ctx, userID, cardID)
		if err != nil {
			return "", err
		}
	}
	// config、err 是与保存操作共用的保留、替换、清除合并结果。
	config, err := normalizeAPIConfig(raw, existing)
	if err != nil {
		return "", &ValidationError{Message: "API 配置无效，请检查请求头、正文及幂等键设置"}
	}
	return config, nil
}
