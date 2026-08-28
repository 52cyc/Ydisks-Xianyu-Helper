// @vitest-environment jsdom
import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, test, vi } from 'vitest';

import { useLiveLogs } from './hooks';
import type { LiveLogsSnapshot } from './api';

// fetchLiveLogsMock 是在模块替换提升前创建的可控日志 API 替身。
const { fetchLiveLogsMock } = vi.hoisted(/* liveLogsMockHoist 创建可被提升 mock factory 安全引用的替身。 */ () => ({ fetchLiveLogsMock: vi.fn() }));

vi.mock('./api', /* liveLogsApiMockFactory 将 Hook 的网络依赖替换为确定性快照。 */ () => ({
  fetchLiveLogs: fetchLiveLogsMock,
}));

/** deferred 创建可由测试显式决定完成顺序的 Promise。 */
const deferred = <T,>() => {
  // resolve 由测试在指定时机完成 Promise。
  let resolve!: (value: T) => void;
  // promise 是供 Hook 等待的可控异步结果。
  const promise = new Promise<T>(/* promiseExecutor 暴露完成回调但不自动解析。 */ completion => { resolve = completion; });
  return { promise, resolve };
};

describe('useLiveLogs', /* liveLogsHookSuite 验证增量、暂停和晚到响应隔离。 */ () => {
  afterEach(/* hookCleanup 恢复计时器并清空每个场景的 API 替身。 */ () => {
    vi.useRealTimers();
    fetchLiveLogsMock.mockReset();
  });

  test('读取新日志并只清空当前视图', /* successCase 覆盖首次读取与清屏交互。 */ async () => {
    fetchLiveLogsMock.mockResolvedValue({ lines: [{ sequence: 1, text: 'server ready' }], nextCursor: 1, oldestCursor: 1 });
    // hook 是当前实时日志状态容器。
    const hook = renderHook(/* hookRenderer 挂载真实实时日志 Hook。 */ () => useLiveLogs());
    await waitFor(/* loadedAssertion 等待首次快照进入页面。 */ () => expect(hook.result.current.lines).toHaveLength(1));
    expect(hook.result.current.connected).toBe(true);
    act(/* clearAction 触发只影响浏览器视图的清空。 */ () => hook.result.current.clearVisible());
    expect(hook.result.current.lines).toEqual([]);
    hook.unmount();
  });

  test('暂停时停止轮询并在继续后立即读取', /* pauseCase 覆盖用户主动暂停和恢复。 */ async () => {
    vi.useFakeTimers();
    fetchLiveLogsMock.mockResolvedValue({ lines: [], nextCursor: 0, oldestCursor: 1 });
    // hook 是计时器控制下的实时日志状态。
    const hook = renderHook(/* hookRenderer 挂载含轮询计时器的 Hook。 */ () => useLiveLogs());
    await act(/* initialFlush 让首次已解析请求完成状态更新。 */ async () => { await Promise.resolve(); });
    act(/* pauseAction 暂停轮询并取消当前计时器。 */ () => hook.result.current.togglePaused());
    // callsAfterPause 是暂停刚生效时的 API 调用数基线。
    const callsAfterPause = fetchLiveLogsMock.mock.calls.length;
    act(/* pausedTimeAdvance 推进两个轮询周期以确认不会读取。 */ () => vi.advanceTimersByTime(3000));
    expect(fetchLiveLogsMock).toHaveBeenCalledTimes(callsAfterPause);
    act(/* resumeAction 恢复实时轮询。 */ () => hook.result.current.togglePaused());
    await act(/* resumeFlush 让恢复 effect 的立即请求完成。 */ async () => { await Promise.resolve(); });
    expect(fetchLiveLogsMock.mock.calls.length).toBeGreaterThan(callsAfterPause);
    hook.unmount();
  });

  test('晚到的旧请求不覆盖最新日志', /* staleCase 覆盖请求世代隔离。 */ async () => {
    // first 代表即将被新请求取消的旧日志读取。
    const first = deferred<LiveLogsSnapshot>();
    // second 代表应该最终更新页面的最新手动刷新。
    const second = deferred<LiveLogsSnapshot>();
    fetchLiveLogsMock.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
    // hook 挂载后立即启动 first 请求。
    const hook = renderHook(/* hookRenderer 挂载可控响应顺序的 Hook。 */ () => useLiveLogs());
    await act(/* secondRequestAction 在首次请求未完成时发起新请求。 */ async () => { void hook.result.current.refresh(); });
    await act(/* staleResolution 故意先返回已被取消的旧日志。 */ async () => {
      first.resolve({ lines: [{ sequence: 1, text: 'stale' }], nextCursor: 1, oldestCursor: 1 });
      await first.promise;
    });
    await act(/* currentResolution 再返回当前请求的最新日志。 */ async () => {
      second.resolve({ lines: [{ sequence: 2, text: 'current' }], nextCursor: 2, oldestCursor: 1 });
      await second.promise;
    });
    expect(hook.result.current.lines.map(/* line 是当前实际保留的日志。 */ line => line.text)).toEqual(['current']);
    hook.unmount();
  });
});
