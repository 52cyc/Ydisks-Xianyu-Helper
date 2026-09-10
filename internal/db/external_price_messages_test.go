package db

import (
	"context"
	"testing"
	"time"
)

// TestExternalPriceMessageDeliveryLifecycle 验证普通通知的首次领取、失败重领和发送成功后的永久防重。
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
	// record 是同一改价通知在全部尝试中复用的稳定投递身份。
	record := ExternalPriceMessageRecord{DedupeKey: "adjusted:1:order", CookieID: "message-account", ChatID: "chat", ItemID: "item", RuleID: 1, MessageKind: "adjusted_notice"}
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

// TestExternalPriceGuidanceCanBeRequotedAfterValidity 验证咨询报价有效期内防重，过期后可由同一会话原子重新领取。
func TestExternalPriceGuidanceCanBeRequotedAfterValidity(t *testing.T) {
	// store 和 cleanup 是迁移到最新结构的隔离仓储及连接清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是用户、账号和报价投递共用的测试上下文。
	ctx := context.Background()
	if _, createErr := store.Users.Create(ctx, "guidance-admin", "guidance@example.com", "pw"); createErr != nil { // createErr 是测试用户创建错误。
		t.Fatal(createErr)
	}
	// owner 和 ownerErr 是报价测试账号的所有者及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "guidance-admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	if saveErr := store.Cookies.Save(ctx, "guidance-account", "unb=1", owner.ID); saveErr != nil { // saveErr 是测试闲鱼账号持久化错误。
		t.Fatal(saveErr)
	}
	// record 是同一规则和聊天会话复用的咨询报价投递身份。
	record := ExternalPriceMessageRecord{DedupeKey: "guide:2:chat", CookieID: "guidance-account", ChatID: "chat", ItemID: "item", RuleID: 2, MessageKind: "guidance"}
	if claimed, claimErr := store.Automation.ClaimExternalPriceGuidance(ctx, record); claimErr != nil || !claimed { // claimed 和 claimErr 验证首次咨询取得发送租约。
		t.Fatalf("首次报价领取失败: claimed=%v err=%v", claimed, claimErr)
	}
	if finishErr := store.Automation.FinishExternalPriceGuidance(ctx, record.DedupeKey, "sent", "", 5*time.Minute); finishErr != nil { // finishErr 是五分钟报价有效期保存错误。
		t.Fatal(finishErr)
	}
	if claimed, claimErr := store.Automation.ClaimExternalPriceGuidance(ctx, record); claimErr != nil || claimed { // claimed 和 claimErr 验证有效期内的重复消息不会再报价。
		t.Fatalf("有效期内不应重新领取: claimed=%v err=%v", claimed, claimErr)
	}
	if _, expireErr := store.DB.ExecContext(ctx, `UPDATE external_price_message_records SET lease_expires_at=1 WHERE dedupe_key=?`, record.DedupeKey); expireErr != nil { // expireErr 是测试夹具把报价有效期推进到过去的写入错误。
		t.Fatal(expireErr)
	}
	if claimed, claimErr := store.Automation.ClaimExternalPriceGuidance(ctx, record); claimErr != nil || !claimed { // claimed 和 claimErr 验证过期后下一条咨询可重新领取。
		t.Fatalf("报价过期后重领失败: claimed=%v err=%v", claimed, claimErr)
	}
}
