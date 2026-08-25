import React, { useRef, useState } from 'react';
import { AlertTriangle, CheckCircle2, Database, DatabaseBackup, Download, RotateCcw, Upload } from 'lucide-react';

import { downloadDataBackup, importDataBackup, type DataRestoreResult } from '../api';

/** formatBytes 将数据库字节数转换为页面可读容量，保留最多一位小数。 */
const formatBytes = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
};

/** DataBackup 提供管理员 SQLite 数据库备份下载与安全恢复暂存页面。 */
const DataBackup: React.FC = () => {
  /** selectedFile 是用户尚未上传的本地数据库文件，不会在页面刷新后保留。 */
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  /** confirmation 是危险恢复操作的短暂人工确认文本。 */
  const [confirmation, setConfirmation] = useState('');
  /** exporting 表示服务端正在创建并下载一致性快照。 */
  const [exporting, setExporting] = useState(false);
  /** importing 表示恢复文件正在上传、校验与暂存。 */
  const [importing, setImporting] = useState(false);
  /** errorMessage 是最近一次请求的可见错误，不包含服务端敏感路径。 */
  const [errorMessage, setErrorMessage] = useState('');
  /** restoreResult 是本次页面会话最近一次成功暂存结果。 */
  const [restoreResult, setRestoreResult] = useState<DataRestoreResult | null>(null);
  /** fileInputRef 指向隐藏文件输入，用于自定义选择区域触发系统文件选择器。 */
  const fileInputRef = useRef<HTMLInputElement>(null);

  /** handleDownload 由下载按钮触发，完成后浏览器保存数据库快照。 */
  const handleDownload = async (): Promise<void> => {
    setExporting(true);
    setErrorMessage('');
    try {
      await downloadDataBackup();
    } catch (error /* error 是备份生成、网络或浏览器保存失败原因。 */) {
      setErrorMessage(error instanceof Error ? error.message : '下载备份失败，请稍后重试');
    } finally {
      setExporting(false);
    }
  };

  /** handleFileChange 由系统文件选择器触发，并清除上一份恢复结果。 */
  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>): void => {
    // nextFile 是本次选择的首个数据库文件；取消选择时恢复为空。
    const nextFile = event.target.files?.[0] ?? null;
    setSelectedFile(nextFile);
    setRestoreResult(null);
    setErrorMessage('');
  };

  /** handleRestore 仅在文件和 RESTORE 确认同时满足时上传并暂存恢复文件。 */
  const handleRestore = async (): Promise<void> => {
    if (!selectedFile || confirmation !== 'RESTORE') return;
    setImporting(true);
    setErrorMessage('');
    try {
      // result 是服务端完成 SQLite 完整性与核心表校验后的暂存状态。
      const result = await importDataBackup(selectedFile);
      setRestoreResult(result);
      setSelectedFile(null);
      setConfirmation('');
      if (fileInputRef.current) fileInputRef.current.value = '';
    } catch (error /* error 是上传、校验或暂存失败原因。 */) {
      setErrorMessage(error instanceof Error ? error.message : '导入备份失败，请确认文件有效');
    } finally {
      setImporting(false);
    }
  };

  return (
    <section className="space-y-8" aria-labelledby="data-backup-title">
      <header className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <div className="mb-3 flex h-11 w-11 items-center justify-center rounded-2xl bg-blue-100 text-blue-700">
            <DatabaseBackup className="h-5 w-5" aria-hidden="true" />
          </div>
          <h1 id="data-backup-title" className="text-4xl font-extrabold tracking-tight text-gray-900">数据备份与恢复</h1>
          <p className="mt-2 font-medium text-gray-500">备份账号、商品、订单、卡密、规则和系统配置数据库。</p>
        </div>
        <div className="rounded-xl border border-slate-200 bg-white px-4 py-3 text-sm font-semibold text-slate-600 shadow-sm">
          当前支持 SQLite 本地数据库
        </div>
      </header>

      {errorMessage && (
        <div className="flex items-start gap-3 rounded-2xl border border-red-200 bg-red-50 px-5 py-4 text-sm font-semibold text-red-700" role="alert">
          <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0" aria-hidden="true" />
          <span>{errorMessage}</span>
        </div>
      )}

      {restoreResult && (
        <div className="flex items-start gap-3 rounded-2xl border border-emerald-200 bg-emerald-50 px-5 py-4 text-sm text-emerald-800" role="status">
          <CheckCircle2 className="mt-0.5 h-5 w-5 shrink-0" aria-hidden="true" />
          <div>
            <p className="font-bold">恢复文件已安全暂存</p>
            <p className="mt-1 font-medium">{restoreResult.filename} · {formatBytes(restoreResult.size)}。{restoreResult.message}</p>
          </div>
        </div>
      )}

      <div className="grid gap-6 lg:grid-cols-2">
        <article className="flex min-h-[25rem] flex-col rounded-3xl border border-slate-200 bg-white p-7 shadow-sm">
          <div className="flex items-center gap-4">
            <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-blue-50 text-blue-700">
              <Download className="h-5 w-5" aria-hidden="true" />
            </div>
            <div>
              <h2 className="text-xl font-extrabold text-slate-900">下载完整数据备份</h2>
              <p className="mt-1 text-sm font-medium text-slate-500">在线生成一致性 SQLite 快照，不需要停止服务。</p>
            </div>
          </div>
          <div className="mt-7 space-y-3 text-sm font-medium text-slate-600">
            <p className="flex gap-3"><CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-600" aria-hidden="true" />包含数据库内全部业务数据和已加密配置。</p>
            <p className="flex gap-3"><CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-600" aria-hidden="true" />自动合并 SQLite WAL 中已经提交的数据。</p>
            <p className="flex gap-3"><AlertTriangle className="h-5 w-5 shrink-0 text-amber-600" aria-hidden="true" />不包含 Chromium、上传文件和数据加密密钥 data-key。</p>
          </div>
          <button type="button" onClick={handleDownload} disabled={exporting || importing} className="mt-auto inline-flex h-12 cursor-pointer items-center justify-center gap-2 rounded-xl bg-blue-700 px-5 text-sm font-extrabold text-white shadow-sm transition-colors hover:bg-blue-800 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-blue-200 disabled:cursor-not-allowed disabled:opacity-50">
            {exporting ? <RotateCcw className="h-4 w-4 animate-spin" aria-hidden="true" /> : <Download className="h-4 w-4" aria-hidden="true" />}
            {exporting ? '正在生成备份…' : '下载数据库备份'}
          </button>
        </article>

        <article className="flex min-h-[25rem] flex-col rounded-3xl border border-red-200 bg-white p-7 shadow-sm">
          <div className="flex items-center gap-4">
            <div className="flex h-12 w-12 items-center justify-center rounded-2xl bg-red-50 text-red-700">
              <Upload className="h-5 w-5" aria-hidden="true" />
            </div>
            <div>
              <h2 className="text-xl font-extrabold text-slate-900">导入并恢复数据</h2>
              <p className="mt-1 text-sm font-medium text-slate-500">先校验并暂存，下次重启服务时替换数据库。</p>
            </div>
          </div>

          <button type="button" onClick={/* filePickerAction 打开系统数据库文件选择器。 */ () => fileInputRef.current?.click()} disabled={importing} className="mt-6 flex cursor-pointer items-center gap-4 rounded-2xl border-2 border-dashed border-slate-300 bg-slate-50 p-5 text-left transition-colors hover:border-blue-400 hover:bg-blue-50 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-blue-100 disabled:cursor-not-allowed disabled:opacity-50">
            <Database className="h-8 w-8 shrink-0 text-slate-500" aria-hidden="true" />
            <span className="min-w-0">
              <span className="block truncate text-sm font-extrabold text-slate-800">{selectedFile?.name ?? '选择 .db 数据库备份'}</span>
              <span className="mt-1 block text-xs font-semibold text-slate-500">{selectedFile ? formatBytes(selectedFile.size) : '最大 512 MiB，上传后先执行完整性校验'}</span>
            </span>
          </button>
          <input ref={fileInputRef} type="file" accept=".db,.sqlite,.sqlite3,application/vnd.sqlite3" onChange={handleFileChange} className="sr-only" aria-label="选择数据库备份文件" />

          <label className="mt-5 block text-sm font-bold text-slate-700" htmlFor="restore-confirmation">
            输入 <span className="font-mono text-red-700">RESTORE</span> 确认恢复
          </label>
          <input id="restore-confirmation" value={confirmation} onChange={/* confirmationChange 同步用户输入的危险操作确认词。 */ event => setConfirmation(event.target.value)} autoComplete="off" spellCheck={false} placeholder="RESTORE" className="mt-2 h-11 rounded-xl border border-slate-300 px-4 font-mono text-sm font-bold text-slate-900 outline-none transition focus:border-red-500 focus:ring-4 focus:ring-red-100" />
          <p className="mt-3 text-xs font-semibold leading-5 text-slate-500">恢复生效后，系统会保留一份恢复前数据库安全副本。请同时保留当前安装的 data-key，否则跨机器恢复后加密配置无法读取。</p>
          <button type="button" onClick={handleRestore} disabled={!selectedFile || confirmation !== 'RESTORE' || importing || exporting} className="mt-auto inline-flex h-12 cursor-pointer items-center justify-center gap-2 rounded-xl bg-red-700 px-5 text-sm font-extrabold text-white shadow-sm transition-colors hover:bg-red-800 focus-visible:outline-none focus-visible:ring-4 focus-visible:ring-red-200 disabled:cursor-not-allowed disabled:opacity-40">
            {importing ? <RotateCcw className="h-4 w-4 animate-spin" aria-hidden="true" /> : <Upload className="h-4 w-4" aria-hidden="true" />}
            {importing ? '正在校验并暂存…' : '导入恢复文件'}
          </button>
        </article>
      </div>
    </section>
  );
};

export default DataBackup;
