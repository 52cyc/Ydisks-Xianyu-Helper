package kasushou

import (
	"context"
	"strings"
	"sync"
	"time"
)

// defaultRequestInterval 为卡速售“10 秒最多 3 次”限制保留调度余量，避免时钟与网络抖动在边界上触发限频。
const defaultRequestInterval = 3500 * time.Millisecond

// rateLimitWaiter 在 ctx 允许的生命周期内等待指定时长；返回值仅表示等待成功或取消。
type rateLimitWaiter func(ctx context.Context, delay time.Duration) error

// requestRateLimiter 为每个卡速售站点与商户组合串行预留请求启动时间。
// mu 只保护 nextByScope；等待和 HTTP I/O 均在锁外执行，同一个 Client 可被多个业务协程并发使用。
type requestRateLimiter struct {
	// mu 保护各限速范围的时间槽和串行门，不得持锁等待或请求网络。
	mu sync.Mutex
	// nextByScope 以站点和商户组合为键，保存已预留队列尾部之后的最早启动时间。
	nextByScope map[string]time.Time
	// gates 以同一限速键保存容量为一的请求门，保证慢响应期间不会重叠发起新请求。
	gates map[string]chan struct{}
	// interval 是同一限速范围内相邻两次真实 HTTP 请求的最小启动间隔。
	interval time.Duration
	// now 提供预留起点时间，生产使用单调时钟，测试可注入可控时间。
	now Clock
	// waiter 在锁外执行可取消等待，测试可用无真实延时的记录器替换。
	waiter rateLimitWaiter
}

// newRequestRateLimiter 创建按 interval 排队的限速器，返回对象安全供并发请求共享。
func newRequestRateLimiter(interval time.Duration) *requestRateLimiter {
	return &requestRateLimiter{
		nextByScope: make(map[string]time.Time),
		gates:       make(map[string]chan struct{}),
		interval:    interval,
		now:         time.Now,
		waiter:      waitForRateLimit,
	}
}

// Acquire 串行取得 scope 的请求权并完成启动间隔等待；返回的 release 必须在 HTTP 处理完成后调用。
func (limiter *requestRateLimiter) Acquire(ctx context.Context, scope string) (func(), error) {
	if limiter == nil || limiter.interval <= 0 {
		return func() {}, nil
	}
	// gate 是当前站点商户共享的单请求通行证。
	gate := limiter.gateForScope(scope)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-gate:
	}
	// release 将单个通行证归还给后续请求，调用方只在成功取得后收到它。
	release := func() { gate <- struct{}{} }
	if waitErr := limiter.Wait(ctx, scope); waitErr != nil { // waitErr 是取得串行权后等待启动时间槽的取消原因。
		release()
		return nil, waitErr
	}
	return release, nil
}

// gateForScope 在短锁内返回 scope 的串行门，首次创建时放入唯一通行证。
func (limiter *requestRateLimiter) gateForScope(scope string) chan struct{} {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if gate := limiter.gates[scope]; gate != nil { // gate 是已有请求共享的通行证通道。
		return gate
	}
	// gate 是新限速键的容量一通道，初始通行证允许首次请求立即进入。
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	limiter.gates[scope] = gate
	return gate
}

// Wait 为 scope 预留一个请求启动时间，并在锁外等待；ctx 取消时不会发起后续 HTTP 请求。
func (limiter *requestRateLimiter) Wait(ctx context.Context, scope string) error {
	if limiter == nil || limiter.interval <= 0 {
		return nil
	}
	// now 是当前请求加入限速队列时的时钟值。
	now := limiter.now()
	// delay 是按已有预留顺序计算的锁外等待时长。
	delay := limiter.reserveDelay(scope, now)
	if delay <= 0 {
		return nil
	}
	return limiter.waiter(ctx, delay)
}

// reserveDelay 在不做任何外部 I/O 的前提下预留 scope 的下一个时间槽，返回调用方应等待的时长。
func (limiter *requestRateLimiter) reserveDelay(scope string, now time.Time) time.Duration {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	// startAt 是本次请求在当前排队中被允许启动的时间。
	startAt := now
	if nextAt := limiter.nextByScope[scope]; nextAt.After(startAt) { // nextAt 是同一站点商户已预留队列的尾部时间。
		startAt = nextAt
	}
	limiter.nextByScope[scope] = startAt.Add(limiter.interval)
	return startAt.Sub(now)
}

// waitForRateLimit 使用可停止计时器等待 delay，ctx 取消时立即返回原始取消原因。
func waitForRateLimit(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	// timer 只属于本次等待，无论到期或取消都在返回前释放。
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// rateLimitScope 用 baseURL 站点地址和 merchantUserID 商户标识构造限速键，不将 API Key 保存到常驻内存映射或日志。
func rateLimitScope(baseURL, merchantUserID string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(baseURL)), "/") + "\x00" + strings.TrimSpace(merchantUserID)
}
