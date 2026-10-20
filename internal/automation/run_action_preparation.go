package automation

import (
	"context"
	"time"

	"xianyu-go/internal/db"
)

// prepareRunAction 处理当前动作的延迟和聊天输入准备；返回更新后任务、是否已持久化延期以及准备错误。
func (r automationRunCoordinator) prepareRunAction(ctx context.Context, task Task, ruleID int64, run *db.AutomationRun, action db.AutomationAction, cursor int, skipDelays bool) (Task, bool, error) {
	if !skipDelays {
		// delaySeconds 是当前动作生效前的等待秒数。
		delaySeconds, delayErr := r.actionDelaySeconds(ctx, action)
		if delayErr != nil {
			return task, false, delayErr
		}
		if delaySeconds > 0 && taskDelayCursor(task) != cursor {
			if task.Raw == nil {
				task.Raw = map[string]any{}
			}
			task.Raw["automation_run_id"] = run.ID
			task.Raw["automation_rule_id"] = ruleID
			task.Raw["automation_delay_cursor"] = cursor
			// dueAt 是延迟动作重新进入可执行状态的 UTC 时间点，同时用于续租当前运行。
			dueAt := time.Now().UTC().Add(time.Duration(delaySeconds) * time.Second)
			if leaseErr := r.store.Automation.RenewRunLease(ctx, run.ID, run.AttemptCount, dueAt.Add(5*time.Minute).Unix()); leaseErr != nil { // leaseErr 是延期运行续租失败原因。
				return task, false, leaseErr
			}
			if deferErr := r.deferTask(ctx, task, dueAt.Unix()); deferErr != nil { // deferErr 是延迟任务写入失败原因。
				return task, false, deferErr
			}
			return task, true, nil
		}
	}
	if r.prepareAction == nil {
		return task, false, nil
	}
	// preparedTask、waiting、prepareErr 是当前动作的聊天输入准备结果。
	preparedTask, waiting, prepareErr := r.prepareAction(ctx, task, run.ID, action)
	if prepareErr != nil {
		return task, false, prepareErr
	}
	task = preparedTask
	if !waiting {
		return task, false, nil
	}
	if task.Raw == nil {
		task.Raw = map[string]any{}
	}
	task.Raw["automation_run_id"] = run.ID
	task.Raw["automation_rule_id"] = ruleID
	// dueAt 仅作为服务端最长保底期，买家确认时会立即把任务唤醒。
	dueAt := time.Now().UTC().Add(365 * 24 * time.Hour)
	if leaseErr := r.store.Automation.RenewRunLease(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(5*time.Minute).Unix()); leaseErr != nil { // leaseErr 是等待买家期间的运行续租错误。
		return task, false, leaseErr
	}
	if deferErr := r.deferTask(ctx, task, dueAt.Unix()); deferErr != nil { // deferErr 是等待聊天确认任务的持久化错误。
		return task, false, deferErr
	}
	return task, true, nil
}
