package automation

import "context"

// PublishRuleRepository 定义批量发布成功后幂等准备自动化规则所需的最小持久化能力。
type PublishRuleRepository interface {
	// EnsurePublishRule 确保同一发布商品的自动化规则只创建一次。
	EnsurePublishRule(ctx context.Context, input RuleInput) error
}

// PublishRuleCloneRepository 在普通发布规则幂等写入之上，提供账号间商品克隆所需的精确规则快照。
type PublishRuleCloneRepository interface {
	PublishRuleRepository
	// ListItemRules 返回当前用户下精确绑定指定账号和商品的规则，不包含账号通用规则。
	ListItemRules(ctx context.Context, userID int64, cookieID, itemID string) ([]Rule, error)
	// EnsureClonedPublishRule 按源规则标识幂等创建目标商品规则，批次恢复时不得重复写入。
	EnsureClonedPublishRule(ctx context.Context, sourceRuleID int64, input RuleInput) error
}

// PublishRuleService 编排发布成功后的自动化规则幂等准备，不依赖 HTTP 或数据库模型。
type PublishRuleService struct {
	// repository 保存调用方注入的发布自动化规则持久化端口。
	repository PublishRuleRepository
}

// NewPublishRuleService 构造批量发布自动化规则应用服务。
func NewPublishRuleService(repository PublishRuleRepository) *PublishRuleService {
	return &PublishRuleService{repository: repository}
}

// Ensure 确保发布自动化规则存在，并保留适配器返回的底层错误语义。
func (s *PublishRuleService) Ensure(ctx context.Context, input RuleInput) error {
	if s == nil || s.repository == nil {
		return ErrInvalidInput
	}
	return s.repository.EnsurePublishRule(ctx, input)
}
