package kasushou

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"
)

// TestRequestRateLimiterKeepsThreeRequestsOutsideTenSecondWindow 验证同一限速键的第四次请求不会被安排在前三次后的十秒内。
func TestRequestRateLimiterKeepsThreeRequestsOutsideTenSecondWindow(t *testing.T) {
	// limiter 使用生产请求间隔计算同一站点商户的四个启动时间槽。
	limiter := newRequestRateLimiter(defaultRequestInterval)
	// now 是所有预留同时进入队列时的固定时钟。
	now := time.Unix(1700000000, 0)
	// delays 保存同一限速键连续四次预留应等待的时长。
	delays := make([]time.Duration, 4)
	for index := range delays { // index 是当前请求在同一排队中的顺序。
		delays[index] = limiter.reserveDelay("station\x00merchant", now)
	}
	if delays[0] != 0 || delays[1] != defaultRequestInterval || delays[2] != 2*defaultRequestInterval || delays[3] < 10*time.Second {
		t.Fatalf("卡速售限速时间槽不满足 10 秒 3 次: %v", delays)
	}
	if otherDelay := limiter.reserveDelay("station\x00other-merchant", now); otherDelay != 0 { // otherDelay 是另一商户的首次请求等待时长。
		t.Fatalf("不同商户不应共享排队: %s", otherDelay)
	}
}

// TestRequestRateLimiterSerializesConcurrentReservations 验证并发业务入口不会抢到同一个请求时间槽。
func TestRequestRateLimiterSerializesConcurrentReservations(t *testing.T) {
	// limiter 是多个模拟业务协程共享的站点商户限速器。
	limiter := newRequestRateLimiter(defaultRequestInterval)
	// now 固定所有并发预留的入队时间，便于稳定比较时间槽。
	now := time.Unix(1700000000, 0)
	// delays 由每个模拟请求发送自己获得的排队时长，测试函数负责关闭。
	delays := make(chan time.Duration, 4)
	// workers 等待四个并发预留全部完成后再关闭结果通道。
	var workers sync.WaitGroup
	workers.Add(4)
	for index := 0; index < 4; index++ { // index 只表示当前启动的模拟请求序号，不决定实际排队次序。
		go func() { // reservationWorker 并发申请同一限速键的一个时间槽。
			defer workers.Done()
			delays <- limiter.reserveDelay("station\x00merchant", now)
		}()
	}
	workers.Wait()
	close(delays)
	// ordered 是排序后的全部预留时长，用于消除 goroutine 调度顺序差异。
	ordered := make([]time.Duration, 0, 4)
	for delay := range delays { // delay 是当前并发请求获得的时间槽等待时长。
		ordered = append(ordered, delay)
	}
	sort.Slice(ordered, func(left, right int) bool { // left、right 是当前比较的两个时长下标。
		return ordered[left] < ordered[right]
	})
	// expected 是四个并发请求应独占的连续时间槽。
	expected := []time.Duration{0, defaultRequestInterval, 2 * defaultRequestInterval, 3 * defaultRequestInterval}
	for index := range expected { // index 是当前比较的排序后时间槽位置。
		if ordered[index] != expected[index] {
			t.Fatalf("并发预留发生重叠: got=%v want=%v", ordered, expected)
		}
	}
}

// TestWaitForRateLimitHonorsCancellation 验证服务关闭或 HTTP 请求取消能立即中止限速等待。
func TestWaitForRateLimitHonorsCancellation(t *testing.T) {
	// ctx、cancel 提供已取消的请求生命周期，避免测试真实等待。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if waitErr := waitForRateLimit(ctx, time.Hour); !errors.Is(waitErr, context.Canceled) { // waitErr 是限速等待观察到的取消原因。
		t.Fatalf("限速等待未传递 context 取消: %v", waitErr)
	}
}

// TestRequestRateLimiterAcquireHonorsGateCancellation 验证请求在等待同站点商户的串行权时也能立即取消。
func TestRequestRateLimiterAcquireHonorsGateCancellation(t *testing.T) {
	// limiter 使用极短时间间隔，本用例只检查串行门而非时钟等待。
	limiter := newRequestRateLimiter(time.Nanosecond)
	// release 持有当前限速键的唯一通行证，让第二次取得停留在串行门。
	release, acquireErr := limiter.Acquire(context.Background(), "station\x00merchant")
	if acquireErr != nil {
		t.Fatal(acquireErr)
	}
	defer release()
	// canceledContext、cancel 提供已取消的第二次取得生命周期。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, secondErr := limiter.Acquire(canceledContext, "station\x00merchant"); !errors.Is(secondErr, context.Canceled) { // secondErr 是等待通行证时观察到的取消错误。
		t.Fatalf("串行门等待未传递 context 取消: %v", secondErr)
	}
}

// TestRequestRateLimiterAcquireAllowsDisabledLimiter 验证测试或兼容客户端关闭限流时仍返回可安全调用的释放函数。
func TestRequestRateLimiterAcquireAllowsDisabledLimiter(t *testing.T) {
	// limiter 为空接收者，用于覆盖兼容调用分支。
	var limiter *requestRateLimiter
	// release、acquireErr 是关闭限流时的空操作释放器和获取错误。
	release, acquireErr := limiter.Acquire(context.Background(), "")
	if acquireErr != nil || release == nil {
		t.Fatalf("关闭限流未返回安全结果: release=%v err=%v", release != nil, acquireErr)
	}
	release()
}

// TestRateLimitScopeNormalizesStationAndSeparatesMerchants 验证同一站点地址的形式差异共享配额，不同商户仍分开排队。
func TestRateLimitScopeNormalizesStationAndSeparatesMerchants(t *testing.T) {
	// normalized 是经过大小写、空格和尾部斜线归一后的限速键。
	normalized := rateLimitScope(" HTTPS://EXAMPLE.COM/ ", "merchant-a")
	if normalized != rateLimitScope("https://example.com", "merchant-a") {
		t.Fatalf("同一站点未归一到同一限速键: %q", normalized)
	}
	if normalized == rateLimitScope("https://example.com", "merchant-b") {
		t.Fatal("不同商户被错误合并到同一限速键")
	}
}
