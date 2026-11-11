package server

import (
	"context"
	"time"
	itemapp "xianyu-go/internal/application/items"
)

// ItemSinglePublishPort 定义单商品发布用例。
type ItemSinglePublishPort interface {
	PublishSingle(context.Context, itemapp.PublishInput) (itemapp.PublishOutcome, error)
}

// ItemBatchPreviewPort 定义批量发布预检所需能力。
type ItemBatchPreviewPort interface {
	CookieOwned(context.Context, int64, string) (bool, error)
	Preview(context.Context, itemapp.BatchPreviewInput) ([]itemapp.BatchPreviewRow, error)
}

// ItemBatchManagementPort 定义批量发布管理和清理能力。
type ItemBatchManagementPort interface {
	StartBatch(context.Context, int64, string, time.Duration) (string, error)
	ListBatches(context.Context, int64, int) ([]itemapp.BatchInfo, error)
	GetBatch(context.Context, int64, string) (itemapp.BatchDetails, error)
	CancelBatch(context.Context, int64, string) (string, error)
	DeleteBatch(context.Context, int64, string) error
	CleanupExpiredUploads(context.Context, time.Time, int) error
	RetryFailedBatch(context.Context, int64, string, time.Duration) (string, error)
}

// ItemCategoryRecommendationPort 定义批量发布类目推荐能力。
type ItemCategoryRecommendationPort interface {
	Recommend(context.Context, int64, string, string) (itemapp.CategoryRecommendation, error)
}

// ItemBatchPreviewPersistencePort 定义批量预检结果保存能力。
type ItemBatchPreviewPersistencePort interface {
	Persist(context.Context, itemapp.BatchPreviewPersistenceBatch, []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error)
}

// ItemBatchLocalPublishPort 定义批量远端发布后的本地持久化和规则收口能力。
type ItemBatchLocalPublishPort interface {
	Complete(context.Context, int64, itemapp.BatchRow, string, *itemapp.BatchPublishResult) error
	EnsureAutomationRules(context.Context, int64, itemapp.BatchRow, *itemapp.BatchPublishResult) error
}

// ItemSyncPort 定义商品同步能力。
type ItemSyncPort interface {
	SyncAll(context.Context, itemapp.SyncQuery) (itemapp.SyncAllResult, error)
	SyncPage(context.Context, itemapp.SyncQuery) (itemapp.SyncPageResult, error)
}

// ItemCatalogPort 定义商品目录读取能力。
type ItemCatalogPort interface {
	ListForUser(context.Context, int64, string) ([]itemapp.CatalogItem, error)
	ListByCookie(context.Context, string) ([]itemapp.CatalogItem, error)
	Get(context.Context, string, string) (itemapp.CatalogItem, error)
}

// ItemCatalogMutationPort 定义商品目录写入能力。
type ItemCatalogMutationPort interface {
	Create(context.Context, string, itemapp.CatalogWriteInput) error
	Update(context.Context, string, string, itemapp.CatalogPatchInput) error
	Delete(context.Context, string, string) error
	SetMultiSpec(context.Context, string, string, bool) error
	SetMultiQuantity(context.Context, string, string, bool) error
}

// ItemLinkImportPort 定义批量分享链接解析和公开商品详情采集能力。
type ItemLinkImportPort interface {
	// CollectBatch 在当前用户和所选自有账号范围内逐条采集，单条失败保留在结果中。
	CollectBatch(context.Context, itemapp.LinkImportInput) (itemapp.LinkImportResult, error)
}

// ItemAISettingsPort 定义商品级 AI 配置读取与保存能力。
type ItemAISettingsPort interface {
	Get(context.Context, int64, string, string) (itemapp.ItemAISettings, error)
	Save(context.Context, int64, itemapp.ItemAISettings) error
}
