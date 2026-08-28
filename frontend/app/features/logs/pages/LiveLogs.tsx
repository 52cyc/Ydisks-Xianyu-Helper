import React, { useEffect, useMemo, useRef, useState } from 'react';
import { CirclePause, CirclePlay, RefreshCw, Search, Terminal, Trash2 } from 'lucide-react';

import { useLiveLogs } from '../hooks';

/** LiveLogs 提供管理员可暂停、搜索和自动跟随的容器运行日志视图。 */
const LiveLogs: React.FC = () => {
  /** hookState 拥有增量轮询、取消和日志缓冲状态。 */
  const { lines, paused, loading, connected, error, togglePaused, clearVisible, refresh } = useLiveLogs();
  /** query 是当前页面内的本地搜索文本；setQuery 不会向服务端传递内容。 */
  const [query, setQuery] = useState('');
  /** autoFollow 表示新日志到达时是否自动滚动到底部；setAutoFollow 由用户复选框控制。 */
  const [autoFollow, setAutoFollow] = useState(true);
  /** logViewportRef 指向可滚动日志容器，仅用于自动跟随。 */
  const logViewportRef = useRef<HTMLDivElement>(null);
  /** normalizedQuery 是不区分大小写搜索使用的标准文本。 */
  const normalizedQuery = query.trim().toLowerCase();
  /** visibleLines 只在日志或搜索词变化时重算，避免密集轮询期间重复过滤。 */
  const visibleLines = useMemo(/* filterVisibleLines 按本地搜索词过滤日志，空词时保留全部。 */ () => (
    normalizedQuery ? lines.filter(/* line 是当前进行不区分大小写匹配的日志。 */ line => line.text.toLowerCase().includes(normalizedQuery)) : lines
  ), [lines, normalizedQuery]);
  /** logText 将可见日志合并为单一 pre 文本节点，避免为数百行创建独立 DOM。 */
  const logText = visibleLines.map(/* line 是当前合并到终端文本的日志行。 */ line => line.text).join('\n');

  useEffect(/* autoFollowEffect 在新文本到达且用户开启跟随时滚动到底部。 */ () => {
    if (!autoFollow || !logViewportRef.current) return;
    logViewportRef.current.scrollTop = logViewportRef.current.scrollHeight;
  }, [autoFollow, logText]);

  return (
    <section className="space-y-6" aria-labelledby="live-logs-title">
      <header className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
        <div>
          <div className="mb-3 flex h-11 w-11 items-center justify-center rounded-2xl bg-slate-900 text-emerald-300">
            <Terminal className="h-5 w-5" aria-hidden="true" />
          </div>
          <h1 id="live-logs-title" className="text-4xl font-extrabold tracking-tight text-gray-900">实时日志</h1>
          <p className="mt-2 font-medium text-gray-500">查看当前容器进程的最新输出，页面最多保留 1000 行。</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <span className={`inline-flex h-10 items-center gap-2 rounded-xl border px-3 text-sm font-bold ${connected ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-amber-200 bg-amber-50 text-amber-700'}`} role="status">
            <span className={`h-2.5 w-2.5 rounded-full ${connected ? 'bg-emerald-500' : 'bg-amber-500'}`} aria-hidden="true" />
            {paused ? '已暂停' : connected ? '实时连接' : loading ? '正在连接' : '连接异常'}
          </span>
          <button type="button" onClick={togglePaused} className="inline-flex h-10 cursor-pointer items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 text-sm font-bold text-slate-700 transition-colors hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-slate-200">
            {paused ? <CirclePlay className="h-4 w-4" aria-hidden="true" /> : <CirclePause className="h-4 w-4" aria-hidden="true" />}
            {paused ? '继续' : '暂停'}
          </button>
          <button type="button" onClick={/* refreshAction 由用户触发一次立即增量读取。 */ () => void refresh()} className="inline-flex h-10 cursor-pointer items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 text-sm font-bold text-slate-700 transition-colors hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-slate-200">
            <RefreshCw className="h-4 w-4" aria-hidden="true" />
            刷新
          </button>
          <button type="button" onClick={clearVisible} className="inline-flex h-10 cursor-pointer items-center gap-2 rounded-xl border border-slate-200 bg-white px-3 text-sm font-bold text-slate-700 transition-colors hover:bg-red-50 hover:text-red-700 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-red-100">
            <Trash2 className="h-4 w-4" aria-hidden="true" />
            清空屏幕
          </button>
        </div>
      </header>

      {error && <div className="rounded-2xl border border-red-200 bg-red-50 px-5 py-4 text-sm font-semibold text-red-700" role="alert">{error}，系统会继续自动重试。</div>}

      <article className="overflow-hidden rounded-3xl border border-slate-800 bg-slate-950 shadow-xl">
        <div className="flex flex-col gap-3 border-b border-slate-800 bg-slate-900/90 p-4 sm:flex-row sm:items-center sm:justify-between">
          <label className="relative block min-w-0 flex-1 sm:max-w-md">
            <span className="sr-only">搜索日志</span>
            <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-slate-500" aria-hidden="true" />
            <input value={query} onChange={/* queryChange 更新本地日志搜索词。 */ event => setQuery(event.target.value)} placeholder="搜索当前日志…" className="h-10 w-full rounded-xl border border-slate-700 bg-slate-950 pl-10 pr-3 text-sm font-medium text-slate-100 outline-none placeholder:text-slate-500 focus:border-emerald-500 focus:ring-4 focus:ring-emerald-500/10" />
          </label>
          <div className="flex items-center justify-between gap-4 text-xs font-semibold text-slate-400 sm:justify-end">
            <span>{visibleLines.length} / {lines.length} 行</span>
            <label className="flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-2 hover:bg-slate-800">
              <input type="checkbox" checked={autoFollow} onChange={/* autoFollowChange 切换新日志到达时的底部跟随。 */ event => setAutoFollow(event.target.checked)} className="h-4 w-4 rounded border-slate-600 accent-emerald-500" />
              自动跟随
            </label>
          </div>
        </div>
        <div ref={logViewportRef} className="h-[62vh] min-h-[28rem] overflow-auto p-5" tabIndex={0} aria-label="实时日志内容">
          {logText ? (
            <pre className="whitespace-pre-wrap break-all font-mono text-[13px] leading-6 text-slate-200">{logText}</pre>
          ) : (
            <div className="flex h-full min-h-[24rem] items-center justify-center text-center text-sm font-semibold text-slate-500">
              {loading ? '正在读取日志…' : normalizedQuery ? '没有匹配当前搜索词的日志' : '暂无日志输出'}
            </div>
          )}
        </div>
      </article>
      <p className="text-xs font-semibold text-slate-500">“清空屏幕”只清除当前浏览器视图，不会删除 Docker 日志或服务端数据。</p>
    </section>
  );
};

export default LiveLogs;
