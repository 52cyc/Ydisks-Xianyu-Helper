import { contractClient, contractMultipartBody, runContractRequest } from '../../../shared/api-contract/client';
import type { RequestControlOptions } from '../../../shared/http/client';

/** DataRestoreResult 是备份文件校验并暂存后的页面模型。 */
export interface DataRestoreResult {
  /** filename 是服务端确认的安全文件名。 */
  filename: string;
  /** size 是已暂存数据库文件的字节数。 */
  size: number;
  /** restartRequired 表示当前服务是否仍需重启才能应用恢复。 */
  restartRequired: boolean;
  /** message 是服务端返回的无敏感路径操作提示。 */
  message: string;
}

/** downloadDataBackup 下载管理员当前 SQLite 数据库的一致性快照并触发浏览器保存。 */
export const downloadDataBackup = async (options?: RequestControlOptions): Promise<void> => {
  // payload 是 openapi-fetch 按二进制 Content-Type 解析的 Blob；测试环境可能返回字符串夹具。
  const payload = await runContractRequest(/* signal 控制备份生成和下载请求的取消与超时，并要求客户端按 Blob 解析 SQLite。 */ signal => contractClient.GET('/api/v1/admin/data-backup', { signal, parseAs: 'blob' }), {
    ...options,
    timeoutMs: options?.timeoutMs ?? 120_000,
  });
  // backupBlob 是供浏览器下载的一次性二进制对象，不写入本地存储。
  // backupBlob 是客户端按二进制模式解析的 SQLite 快照。
  const backupBlob = payload;
  // downloadURL 是浏览器为本次快照创建的短期对象地址，点击后立即释放。
  const downloadURL = URL.createObjectURL(backupBlob);
  // anchor 是仅用于触发原生文件保存的临时链接节点。
  const anchor = document.createElement('a');
  // timestamp 是用户本地时间生成的文件名后缀，服务端真实路径不会进入浏览器。
  const timestamp = new Date().toISOString().replace(/[:.]/g, '-');
  anchor.href = downloadURL;
  anchor.download = `ydisks-backup-${timestamp}.db`;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(downloadURL);
};

/** importDataBackup 上传并暂存数据库恢复文件；成功仅表示校验通过，实际恢复等待重启。 */
export const importDataBackup = async (file: File, options?: RequestControlOptions): Promise<DataRestoreResult> => {
  // form 是保留文件流和确认口令的原生 multipart 请求体。
  const form = new FormData();
  form.append('file', file);
  form.append('confirmation', 'RESTORE');
  // response 是 OpenAPI 约束的恢复暂存响应，不包含本机文件系统路径。
  const response = await runContractRequest(/* signal 控制大文件上传请求的取消与十分钟超时。 */ signal => contractClient.POST('/api/v1/admin/data-restore', {
    body: contractMultipartBody(form),
    signal,
  }), { ...options, timeoutMs: options?.timeoutMs ?? 600_000 });
  return {
    filename: response.filename,
    size: response.size,
    restartRequired: response.restart_required,
    message: response.message,
  };
};
