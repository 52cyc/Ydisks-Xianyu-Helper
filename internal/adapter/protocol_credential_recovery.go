package adapter

import (
	"context"
	"errors"
	"strings"
	"time"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// tryProtocolCredentialRenew 封装tryProtocolCredentialRenew业务协调。
func (a *Adapter) tryProtocolCredentialRenew(ctx context.Context, d *db.CookiePlatformRuntimeData) (bool, error) {
	if d == nil {
		return false, nil
	}
	// current 用于本次流程后续判断的current
	current := d.Value
	// api 用于本次流程后续判断的api
	api := a.renewSvc
	// 官网始终先由 auto-login plugin 按 havana_lgc_exp/cookie3_bak_exp
	// 决定是否调用 silentHasLogin。Go 客户端复刻该 HTTP 协议，不加载页面。
	// runCtx、cancel 用于本次流程后续判断的运行Ctx、cancel
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	// res、err 用于本次流程后续判断的res、err
	res, err := api.RenewAfterSessionExpired(runCtx, current, cookierefresh.SnapshotFromMetadata(d.MetadataJSON))
	if res != nil {
		if // saveErr 用于本次流程后续判断的saveErr
		saveErr := a.persistProtocolRenewalResponse(ctx, d, res); saveErr != nil {
			return false, saveErr
		}
		if res.HasPending() {
			// 恢复路径不能把“底层请求仍在进行”提前记为成功，否则上层会
			// 重置失败计数并继续使用未确认的旧凭证。这里等待最终响应；定时
			// 调度仍使用异步 watcher，不阻塞健康账号。
			// waitCtx、waitCancel 用于本次流程后续判断的waitCtx、wait取消
			waitCtx, waitCancel := context.WithTimeout(ctx, 35*time.Second)
			// late、waitErr 用于本次流程后续判断的late、waitErr
			late, waitErr := res.AwaitPending(waitCtx)
			waitCancel()
			if late == nil {
				if waitErr != nil {
					return false, waitErr
				}
				return false, errors.New("协议续期底层响应未返回结果")
			}
			if // saveErr 用于本次流程后续判断的saveErr
			saveErr := a.persistProtocolRenewalResponse(ctx, d, late); saveErr != nil {
				return false, saveErr
			}
			if waitErr != nil {
				return false, waitErr
			}
			if late.Success {
				a.logger.Info("Go 协议续期迟到响应成功", "account", d.ID)
				return true, nil
			}
			// message 用于本次流程后续判断的消息
			message := strings.TrimSpace(late.Message)
			if message == "" {
				message = "协议续期未通过"
			}
			return false, errors.New(message)
		}
		if err == nil && res.Success {
			a.logger.Info("Go 协议续期成功", "account", d.ID)
			return true, nil
		}
		if err == nil {
			// message 保存平台续期失败原因；空原因使用稳定的非敏感默认提示。
			message := strings.TrimSpace(res.Message)
			if message == "" {
				message = "协议续期未通过"
			}
			return false, errors.New(message)
		}
	}
	if err == nil {
		err = errors.New("协议续期未返回结果")
	}
	return false, err
}
