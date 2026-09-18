package adapter

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
)

// itemSnapshotClientStub 为外链采集测试提供可控详情能力，其余 MTOP 方法由嵌入接口占位。
type itemSnapshotClientStub struct {
	// Client 提供测试未调用的通用 MTOP 方法集合。
	mtop.Client
	// fetch 保存商品详情采集的可控测试行为。
	fetch func(context.Context, string, string) (mtop.ItemSnapshot, error)
}

// FetchItemSnapshot 调用测试注入的商品详情采集行为。
func (stub itemSnapshotClientStub) FetchItemSnapshot(ctx context.Context, cookies, itemID string) (mtop.ItemSnapshot, error) {
	return stub.fetch(ctx, cookies, itemID)
}

// TestItemPublishPortCollect 验证账号归属、平台快照转换和运行时 Cookie 同步边界。
func TestItemPublishPortCollect(t *testing.T) {
	// store 和 cleanup 保存当前测试的 SQLite 适配器数据库及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// updated 保存响应会话变化同步到运行时的账号和值。
	updated := ""
	// client 返回确定性的公开商品详情快照。
	client := itemSnapshotClientStub{fetch: func(_ context.Context, cookies, itemID string) (mtop.ItemSnapshot, error) {
		if cookies == "" || itemID != "item-1" {
			t.Fatalf("unexpected fetch arguments: cookies=%q itemID=%q", cookies, itemID)
		}
		return mtop.ItemSnapshot{Title: "商品", Description: "描述", PriceText: "9.90", ImageURLs: []string{"https://img.example/a.jpg"}}, nil
	}}
	// port 是绑定测试数据库和平台快照替身的商品基础设施端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, func(_ context.Context, cookieID, value string) {
		updated = cookieID + ":" + value
	}, nil)
	// collected 和 collectErr 保存归属正确账号的详情采集结果。
	collected, collectErr := port.Collect(context.Background(), 1, "cid", "item-1")
	if collectErr != nil || collected.Title != "商品" || collected.Price != "9.90" || len(collected.Images) != 1 {
		t.Fatalf("Collect() = %+v, %v", collected, collectErr)
	}
	if updated != "" {
		t.Fatalf("unchanged cookie should not update runtime: %q", updated)
	}
	if _, ownershipErr := port.Collect(context.Background(), 999, "cid", "item-1"); ownershipErr == nil {
		t.Fatal("foreign account should fail")
	}
}

// TestItemPublishPortCollectErrors 验证能力缺失、平台错误和空端口返回稳定错误。
func TestItemPublishPortCollectErrors(t *testing.T) {
	// store 和 cleanup 保存当前测试的 SQLite 适配器数据库及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// unsupported 是只实现通用 MTOP 接口、不实现快照能力的端口。
	unsupported := NewItemPublishPort(store, func() mtop.Client { return unsupportedItemPublishClient{} }, nil, nil, nil)
	if _, err := unsupported.Collect(context.Background(), 1, "cid", "item-1"); err == nil {
		t.Fatal("unsupported client should fail")
	}
	// platformErr 是详情读取返回的确定性平台错误。
	platformErr := errors.New("商品不可见")
	// failing 是返回平台错误的采集端口。
	failing := NewItemPublishPort(store, func() mtop.Client {
		return itemSnapshotClientStub{fetch: func(context.Context, string, string) (mtop.ItemSnapshot, error) {
			return mtop.ItemSnapshot{}, platformErr
		}}
	}, nil, nil, nil)
	if _, err := failing.Collect(context.Background(), 1, "cid", "item-1"); !errors.Is(err, platformErr) {
		t.Fatalf("platform error = %v", err)
	}
	// emptyPort 是未装配依赖的零值端口。
	var emptyPort *ItemPublishPort
	if _, err := emptyPort.Collect(context.Background(), 1, "cid", "item-1"); err == nil {
		t.Fatal("nil port should fail")
	}
}
