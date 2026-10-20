import { contractClient, runContractRequest } from '../../../shared/api-contract/client';
import { type RequestControlOptions } from '../../../shared/http/client';
import { normalizeRuntimeLogLevel, normalizeRuntimeLogSource } from './models';
import type { RuntimeLogPage, RuntimeLogQuery } from './types';


/** LiveLogLine 是页面使用的单条进程日志模型。 */
export interface LiveLogLine {
  /** sequence 是进程内单调递增的日志游标。 */
  sequence: number;
  /** text 是与容器输出格式一致的单行文本。 */
  text: string;
}

/** LiveLogsSnapshot 是前端轮询使用的增量日志快照。 */
export interface LiveLogsSnapshot {
  /** lines 是当前游标之后的日志行。 */
  lines: LiveLogLine[];
  /** nextCursor 是下次轮询应携带的游标。 */
  nextCursor: number;
  /** oldestCursor 是服务端内存中仍可读取的最旧游标。 */
  oldestCursor: number;
}

/** fetchLiveLogs 读取指定游标后最多 500 条管理员进程日志。 */
export const fetchLiveLogs = async (after: number, options?: RequestControlOptions): Promise<LiveLogsSnapshot> => {
  // response 是 OpenAPI 生成契约校验后的日志快照。
  const response = await runContractRequest(/* signal 使页面暂停或卸载时可取消正在进行的轮询。 */ signal => contractClient.GET('/api/v1/admin/logs', {
    params: { query: { after, limit: 500 } },
    signal,
  }), options);
  return {
    lines: response.lines.map(/* line 是当前转换为页面模型的契约日志行。 */ line => ({ sequence: Number(line.sequence), text: line.text })),
    nextCursor: Number(response.next_cursor),
    oldestCursor: Number(response.oldest_cursor),
  };
};

/** fetchRuntimeLogs 读取运行日志增量页，并把传输 DTO 归一化为页面模型。 */
export const fetchRuntimeLogs = async (
  query: RuntimeLogQuery,
  since: number,
  limit: number,
  options?: RequestControlOptions,
): Promise<RuntimeLogPage> => {
  // data 是契约客户端按生成类型返回的运行日志传输 DTO。
  const data = await runContractRequest(/* signal 是本次运行日志请求的超时与取消控制信号。 */ signal => contractClient.GET('/api/v1/logs/runtime', {
    params: {
      query: {
        since,
        limit,
        level: query.level === '' ? undefined : query.level,
        source: query.source === '' ? undefined : query.source,
        keyword: query.keyword.trim() === '' ? undefined : query.keyword.trim(),
      },
    },
    signal,
  }), options);
  return {
    entries: data.entries.map(/* entry 是服务端返回的一条运行日志传输 DTO。 */ entry => ({
      seq: entry.seq,
      time: entry.time,
      level: normalizeRuntimeLogLevel(entry.level),
      source: normalizeRuntimeLogSource(entry.source),
      message: entry.message,
      method: entry.method ?? '',
      path: entry.path ?? '',
      status: entry.status ?? 0,
      durationMs: entry.duration_ms ?? 0,
      requestId: entry.request_id ?? '',
      api: entry.api ?? '',
      category: entry.category ?? '',
      ret: entry.ret ?? '',
      error: entry.error ?? '',
      accountId: entry.account_id ?? '',
      itemId: entry.item_id ?? '',
      orderId: entry.order_id ?? '',
      chatId: entry.chat_id ?? '',
      details: (entry.details ?? []).map(/* detail 是服务端返回的一条附加属性。 */ detail => ({ key: detail.key, value: detail.value })),
    })),
    nextCursor: data.next_cursor,
    oldestSeq: data.oldest_seq,
    dropped: data.dropped,
    capacity: data.capacity,
  };
};
