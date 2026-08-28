import { useCallback, useEffect, useRef, useState } from 'react';

import { fetchLiveLogs, type LiveLogLine } from './api';

// MAX_VISIBLE_LINES 限制浏览器保留的最新日志行，避免长时间开启页面占用无界内存。
const MAX_VISIBLE_LINES = 1000;
// POLL_INTERVAL_MS 是实时日志的默认增量轮询间隔。
const POLL_INTERVAL_MS = 1500;

/** UseLiveLogsResult 描述实时日志页的服务端数据和短暂交互状态。 */
export interface UseLiveLogsResult {
  /** lines 是浏览器当前保留的最新日志行。 */
  lines: LiveLogLine[];
  /** paused 表示用户是否暂停了网络轮询。 */
  paused: boolean;
  /** loading 表示首次日志快照尚未返回。 */
  loading: boolean;
  /** connected 表示最近一次增量读取已成功。 */
  connected: boolean;
  /** error 是最近一次轮询失败的可见说明。 */
  error: string;
  /** togglePaused 在暂停和继续实时读取之间切换。 */
  togglePaused: () => void;
  /** clearVisible 仅清空当前浏览器视图，不删除容器或服务端日志。 */
  clearVisible: () => void;
  /** refresh 立即发起一次增量读取。 */
  refresh: () => Promise<void>;
}

/** useLiveLogs 管理可取消的单请求轮询、增量游标和最新 1000 行页面日志。 */
export const useLiveLogs = (): UseLiveLogsResult => {
  /** lines 是服务端日志的页面副本；setLines 只追加当前请求世代的结果。 */
  const [lines, setLines] = useState<LiveLogLine[]>([]);
  /** paused 是用户控制的短暂 UI 状态；setPaused 会触发轮询 effect 重建。 */
  const [paused, setPaused] = useState(false);
  /** loading 只覆盖首次读取；setLoading 不在后续轮询时闪烁页面。 */
  const [loading, setLoading] = useState(true);
  /** connected 保存最近一次读取结果；setConnected 为状态文案提供信号。 */
  const [connected, setConnected] = useState(false);
  /** error 保存最近失败说明；setError 在下次成功后清空。 */
  const [error, setError] = useState('');
  /** cursorRef 保存服务端已消费游标，更新它不需要重新渲染。 */
  const cursorRef = useRef(0);
  /** controllerRef 拥有当前唯一轮询请求的取消权。 */
  const controllerRef = useRef<AbortController | null>(null);
  /** mountedRef 防止页面卸载后的晚到结果更改 React 状态。 */
  const mountedRef = useRef(true);

  /** refresh 中止上一次未完成读取，并仅接受最新 controller 的增量结果。 */
  const refresh = useCallback(/* refreshCallback 执行可取消的增量日志读取。 */ async (): Promise<void> => {
    controllerRef.current?.abort();
    // controller 拥有本次增量日志请求的取消信号。
    const controller = new AbortController();
    controllerRef.current = controller;
    try {
      // snapshot 是当前游标后的最新服务端日志。
      const snapshot = await fetchLiveLogs(cursorRef.current, { signal: controller.signal });
      if (!mountedRef.current || controllerRef.current !== controller) return;
      cursorRef.current = snapshot.nextCursor;
      setLines(/* appendCurrentLines 去重追加新日志，并只保留最后 1000 行。 */ currentLines => {
        // knownSequences 用于防止重试响应将同一游标重复加入页面。
        const knownSequences = new Set(currentLines.map(/* line 是已在页面中的日志行。 */ line => line.sequence));
        // appended 是去除已知游标后的顺序日志列表。
        const appended = currentLines.concat(snapshot.lines.filter(/* line 是当前检查是否重复的新日志。 */ line => !knownSequences.has(line.sequence)));
        return appended.slice(-MAX_VISIBLE_LINES);
      });
      setConnected(true);
      setError('');
    } catch (requestError /* requestError 是本次日志读取的取消或网络失败原因。 */) {
      if (!mountedRef.current || controller.signal.aborted || controllerRef.current !== controller) return;
      setConnected(false);
      setError(requestError instanceof Error ? requestError.message : '读取实时日志失败');
    } finally {
      if (mountedRef.current && controllerRef.current === controller) setLoading(false);
    }
  }, []);

  useEffect(/* pollingEffect 在页面可用且未暂停时维持单一增量轮询计时器。 */ () => {
    mountedRef.current = true;
    if (paused) {
      controllerRef.current?.abort();
      return undefined;
    }
    void refresh();
    // timer 每 1.5 秒触发一次增量读取，refresh 会中止尚未完成的上一请求。
    const timer = window.setInterval(/* pollTick 在定时周期到达时读取新日志。 */ () => void refresh(), POLL_INTERVAL_MS);
    return /* pollingCleanup 在暂停或页面卸载时释放计时器和请求。 */ () => {
      window.clearInterval(timer);
      controllerRef.current?.abort();
    };
  }, [paused, refresh]);

  useEffect(/* mountEffect 在页面最终卸载时禁止任何晚到响应更新状态。 */ () => /* finalUnmountCleanup 标记组件已离开并取消在途请求。 */ () => {
    mountedRef.current = false;
    controllerRef.current?.abort();
  }, []);

  /** togglePaused 使用函数式更新切换轮询状态。 */
  const togglePaused = (): void => setPaused(/* pausedState 是切换前的暂停状态。 */ pausedState => !pausedState);
  /** clearVisible 仅清除浏览器日志数组，保留游标以免重新下载旧日志。 */
  const clearVisible = (): void => setLines([]);

  return { lines, paused, loading, connected, error, togglePaused, clearVisible, refresh };
};
