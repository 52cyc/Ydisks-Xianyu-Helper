package notify

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// prepareErrorRepository 仅替换渠道读取结果，用于验证预备通知失败不会隐式执行入队。
type prepareErrorRepository struct {
	// queueRepository 提供可检测的入队记录及其它仓储方法。
	*queueRepository
	// channelErr 是渠道查询应返回的错误。
	channelErr error
}

// AccountChannels 返回预置错误；ctx 和 accountID 沿用通知仓储接口但此隔离替身不访问外部服务。
func (r *prepareErrorRepository) AccountChannels(ctx context.Context, accountID string) ([]db.NotificationChannel, error) {
	return nil, r.channelErr
}

// TestPrepareAutomationRunOutboxIsReadOnly 验证准备通知只返回记录、保留人工类别和旧订阅兼容，并传播读取错误。
// t 提供失败报告；通知渠道不含真实配置，不进行网络发送或数据库写入。
func TestPrepareAutomationRunOutboxIsReadOnly(t *testing.T) {
	// repository 同时提供旧付款订阅、独立人工订阅及不匹配订阅，捕获意外入队。
	repository := &queueRepository{scriptedOutboxRepository: &scriptedOutboxRepository{}, channels: []db.NotificationChannel{
		{ID: 1, Type: "webhook", EventTypes: EventAutomationOrderPaid},
		{ID: 2, Type: "webhook", EventTypes: EventManualInterventionRequired},
		{ID: 3, Type: "webhook", EventTypes: EventAutomationBuyerReviewed},
	}}
	// notifier 仅准备记录，未启动任何 worker。
	notifier := NewWithRepository("cid", repository, nilLogger())
	// messages、prepareErr 保存用于业务事务的通知记录与渠道查询错误。
	messages, prepareErr := notifier.PrepareAutomationRunOutbox(context.Background(), "order_paid", 81, "cid", "buyer", "item", "needs_review", "缺少发货凭证", "chat")
	if prepareErr != nil || len(messages) != 2 || repository.enqueued != nil {
		t.Fatalf("prepare count=%d enqueued=%d err=%v", len(messages), len(repository.enqueued), prepareErr)
	}
	// message 是匹配旧类别或人工类别订阅的一条预备消息。
	for _, message := range messages {
		if message.EventType != EventManualInterventionRequired || message.IdempotencyKey != "automation-run:81:needs_review" || !strings.Contains(message.Body, "缺少发货凭证") {
			t.Fatal("预备记录必须保留人工处理类别、稳定幂等键与业务原因")
		}
	}
	// noContext 表示调用方未提供生命周期上下文，必须返回空记录且不 panic。
	var noContext context.Context
	messages, prepareErr = notifier.PrepareAutomationRunOutbox(noContext, "order_paid", 81, "cid", "", "", "needs_review", "", "")
	if prepareErr != nil || len(messages) != 0 {
		t.Fatal("缺少上下文不得准备通知")
	}
	// readErr 保存渠道读取失败，调用方应保留原业务状态并等待重试。
	readErr := errors.New("channel read unavailable")
	// failingRepository 只覆盖渠道读取，以检测失败路径不会意外写入通知。
	failingRepository := &prepareErrorRepository{queueRepository: repository, channelErr: readErr}
	messages, prepareErr = NewWithRepository("cid", failingRepository, nilLogger()).PrepareAutomationRunOutbox(context.Background(), "order_paid", 81, "cid", "", "", "needs_review", "", "")
	if !errors.Is(prepareErr, readErr) || len(messages) != 0 || repository.enqueued != nil {
		t.Fatal("渠道查询失败必须传播错误且不写通知")
	}
}
