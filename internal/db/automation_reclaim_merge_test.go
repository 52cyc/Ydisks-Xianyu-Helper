package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestReclaimRunPreservesExternalWaitAndRefundGates 验证同键重领同时保留 fork 长轮询预算与上游退款撤回安全限制。
// t 负责隔离数据库和断言；所有外部等待仅为本地运行状态，不发起供应商请求。
func TestReclaimRunPreservesExternalWaitAndRefundGates(t *testing.T) {
	// store、cleanup 保存临时数据库与本测试负责调用的释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 控制本地数据库调用，测试不创建后台任务。
	ctx := context.Background()
	// userID、cookieID 标识测试规则和运行所属的同一用户与账号。
	userID, cookieID := seedAccount(t, store)
	// ruleID、createErr 保存仅用于测试运行领取的规则及初始化错误。
	ruleID, createErr := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, "merge-item", "order_paid", true, 100))
	if createErr != nil {
		t.Fatal(createErr)
	}
	// scenarios 同时覆盖普通三次预算、外部长轮询预算及已取消运行的副作用围栏。
	scenarios := []struct {
		// status、reason 是历史运行终态和明确的重试/安全取消标记。
		status, reason string
		// attempt、cursor 是历史领取代次及已完成动作数；want 表示是否允许重领。
		attempt, cursor int
		want            bool
	}{
		{"failed", ExternalWaitErrorPrefix + "waiting", 4, 0, true},
		{"failed", ExternalWaitErrorPrefix + "waiting", externalWaitMaxAttempts, 0, false},
		{"failed", SafeRetryErrorPrefix + "retry", 3, 0, false},
		{"canceled", ObsoleteRunReasonMarker + "refunding", 1, 0, true},
		{"canceled", ObsoleteRunReasonMarker + "refunding", 1, 1, false},
		{"canceled", "人工取消", 1, 0, false},
	}
	// index、scenario 分别区分隔离订单与当前历史运行状态。
	for index, scenario := range scenarios {
		// input 为每个用例使用独立订单与幂等键，租约在未来一分钟内有效。
		input := AutomationRun{RuleID: ruleID, CookieID: cookieID, OrderID: fmt.Sprintf("merge-order-%d", index), TriggerType: "order_paid", TriggerKey: fmt.Sprintf("merge:%d", index), LeaseExpiresAt: time.Now().Add(time.Minute).Unix()}
		// runID、started、startErr 保存首次运行创建结果。
		runID, started, startErr := store.Automation.TryStartRun(ctx, input)
		if startErr != nil || !started {
			t.Fatalf("seed case %d: started=%v err=%v", index, started, startErr)
		}
		// updateErr 保存模拟历史终态的数据库错误。
		if _, updateErr := store.DB.ExecContext(ctx, `UPDATE automation_runs SET status=?,error_message=?,attempt_count=?,action_cursor=?,action_started=0,next_retry_at=0 WHERE id=?`, scenario.status, scenario.reason, scenario.attempt, scenario.cursor, runID); updateErr != nil {
			t.Fatal(updateErr)
		}
		// reclaimedID、claimed、claimErr 验证合并后的共享 SQL 分支，不可另建运行规避原幂等键。
		reclaimedID, claimed, claimErr := store.Automation.TryStartRun(ctx, input)
		if claimErr != nil || claimed != scenario.want || claimed && reclaimedID != runID {
			t.Fatalf("case %d: claimed=%v want=%v same_run=%v err=%v", index, claimed, scenario.want, reclaimedID == runID, claimErr)
		}
	}
}
