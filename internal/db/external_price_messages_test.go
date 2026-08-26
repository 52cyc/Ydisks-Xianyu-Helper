package db

import (
	"context"
	"testing"
)

// TestExternalPriceMessageDeliveryLifecycle 验证消息首次领取、失败重领和发送成功后的永久防重。
func TestExternalPriceMessageDeliveryLifecycle(t *testing.T) {
	// store 和 cleanup 是迁移到最新结构的隔离仓储及连接清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是账号创建和消息状态转换共用的测试上下文。
	ctx := context.Background()
	if _, createErr := store.Users.Create(ctx, "message-admin", "message@example.com", "pw"); createErr != nil { // createErr 是测试用户创建错误。
		t.Fatal(createErr)
	}
	// owner 是消息测试账号的本地所有者。
	owner, ownerErr := store.Users.GetByUsername(ctx, "message-admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	if saveErr := store.Cookies.Save(ctx, "message-account", "unb=1", owner.ID); saveErr != nil { // saveErr 是测试闲鱼账号持久化错误。
		t.Fatal(saveErr)
	}
	// record 是同一咨询引导在全部尝试中复用的稳定投递身份。
	record := ExternalPriceMessageRecord{DedupeKey: "guide:1:chat", CookieID: "message-account", ChatID: "chat", ItemID: "item", RuleID: 1, MessageKind: "guidance"}
	if claimed, claimErr := store.Automation.ClaimExternalPriceMessage(ctx, record); claimErr != nil || !claimed { // claimed 和 claimErr 验证首次调用取得发送租约。
		t.Fatalf("首次领取失败: claimed=%v err=%v", claimed, claimErr)
	}
	if finishErr := store.Automation.FinishExternalPriceMessage(ctx, record.DedupeKey, "failed", "测试发送失败"); finishErr != nil { // finishErr 是明确失败状态保存错误。
		t.Fatal(finishErr)
	}
	if claimed, claimErr := store.Automation.ClaimExternalPriceMessage(ctx, record); claimErr != nil || !claimed { // claimed 和 claimErr 验证失败记录可安全重领。
		t.Fatalf("失败重领失败: claimed=%v err=%v", claimed, claimErr)
	}
	if finishErr := store.Automation.FinishExternalPriceMessage(ctx, record.DedupeKey, "sent", ""); finishErr != nil { // finishErr 是发送成功终态保存错误。
		t.Fatal(finishErr)
	}
	if claimed, claimErr := store.Automation.ClaimExternalPriceMessage(ctx, record); claimErr != nil || claimed { // claimed 和 claimErr 验证 sent 记录永久阻止重复发送。
		t.Fatalf("已发送记录不应再次领取: claimed=%v err=%v", claimed, claimErr)
	}
}
