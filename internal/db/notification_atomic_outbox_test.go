package db

import (
	"context"
	"testing"
)

// TestMultiDBQuarantineOutboxAtomicity 验证渠道外键错误会回滚隔离与先前消息，重复幂等键则不阻断合法隔离。
// t 提供各方言隔离数据库；未配置的 MySQL/PostgreSQL 明确跳过，不连接生产库。
func TestMultiDBQuarantineOutboxAtomicity(t *testing.T) {
	// target 是当前可用的数据库目标及其清理责任。
	for _, target := range allTestTargets(t) {
		// t 是当前方言子测试，数据库在子测试结束后释放。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// store、ctx 保存当前隔离数据库仓储和本地操作上下文。
			store, ctx := target.store, context.Background()
			// userID、cookieID 创建通知与付款运行共同所属的测试账号。
			userID, cookieID := seedAccount(t, store)
			// ruleID 是待恢复付款运行所属的启用规则。
			ruleID := seedCatchupRule(t, store, userID, cookieID, "atomic-item", "隔离事务", true, []AutomationActionInput{{ActionType: "confirm_shipment", Enabled: true}})
			// runID 创建仅供事务围栏验证的付款运行，不调用外部动作。
			runID := seedCatchupRun(t, store, ruleID, cookieID, "atomic-order")
			// setupErr 模拟因退款取消且已有发卡进度但缺失凭证的历史运行。
			if _, setupErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET status='canceled',error_message=?,action_cursor=1,action_started=0,delivery_proof='' WHERE id=?`, ObsoleteRunReasonMarker+"退款", runID); setupErr != nil {
				t.Fatal(setupErr)
			}
			// channelID、channelErr 保存有效渠道，配置不含真实地址或凭证。
			channelID, channelErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{Name: "atomic", Type: "webhook", Config: `{}`, Enabled: true, UserID: userID})
			if channelErr != nil {
				t.Fatal(channelErr)
			}
			// valid 是应和隔离状态在同一事务落地的一条消息；invalid 使用不存在渠道触发外键错误。
			valid := NotificationOutboxInput{ChannelID: channelID, EventType: "manual_intervention_required", Body: "缺少发货凭证", IdempotencyKey: "atomic-review"}
			// invalid 在有效消息之后写入，验证失败时连先前成功写入也会撤销。
			invalid := NotificationOutboxInput{ChannelID: channelID + 9999, EventType: valid.EventType, Body: valid.Body, IdempotencyKey: "invalid-review"}
			// quarantined、quarantineErr 保存包含失效渠道时的事务结果，必须报错且不提交状态。
			quarantined, quarantineErr := store.Automation.QuarantineCanceledRunMissingProof(ctx, runID, 1, "缺少发货凭证", []NotificationOutboxInput{valid, invalid})
			if quarantineErr == nil || quarantined {
				t.Fatal("无效渠道必须导致隔离失败，不能被冲突忽略语义吞掉")
			}
			// status、queued 分别验证状态回滚以及事务内先写入消息也已回滚。
			var status string
			// queued 保存通知表中的消息数，错误路径应为零。
			var queued int
			// readErr 保存隔离状态读取错误。
			if readErr := store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE id=?`, runID).Scan(&status); readErr != nil || status != "canceled" {
				t.Fatalf("rollback status=%q err=%v", status, readErr)
			}
			// countErr 保存通知回滚查询错误。
			if countErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_outbox`).Scan(&queued); countErr != nil || queued != 0 {
				t.Fatalf("rollback queued=%d err=%v", queued, countErr)
			}
			// enqueueErr 先写入同一幂等键，以验证重复通知仍能和合法状态变更兼容。
			if enqueueErr := store.Notifications.EnqueueOutbox(ctx, []NotificationOutboxInput{valid}); enqueueErr != nil {
				t.Fatal(enqueueErr)
			}
			quarantined, quarantineErr = store.Automation.QuarantineCanceledRunMissingProof(ctx, runID, 1, "缺少发货凭证", []NotificationOutboxInput{valid})
			if quarantineErr != nil || !quarantined {
				t.Fatalf("duplicate must allow quarantine: changed=%v err=%v", quarantined, quarantineErr)
			}
			// finalErr 验证成功隔离后仍只有原有一条幂等通知。
			if finalErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notification_outbox`).Scan(&queued); finalErr != nil || queued != 1 {
				t.Fatalf("dedupe queued=%d err=%v", queued, finalErr)
			}
		})
	}
}
