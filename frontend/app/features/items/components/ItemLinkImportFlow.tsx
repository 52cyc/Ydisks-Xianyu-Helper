import { AlertTriangle,ArrowRight,CheckCircle2,Link2,LoaderCircle,X } from 'lucide-react';
import React,{ useEffect,useMemo,useRef,useState } from 'react';
import { createPortal } from 'react-dom';
import { collectItemLinks,itemErrorMessage } from '../api';
import { buildItemLinkImportCSV,MAX_ITEM_LINK_IMPORT_SOURCES,parseItemLinkSources } from '../linkImportState';
import type { AccountDetail,ItemLinkImportResponse } from '../models';

// ItemLinkImportFlowProps 描述分享链接批量采集入口依赖的账号与预检动作。
interface ItemLinkImportFlowProps {
  // accounts 是用户可选择的自有闲鱼账号。
  accounts: AccountDetail[];
  // preferredAccountID 是商品页当前筛选或同步的优先账号。
  preferredAccountID: string;
  // batchBusy 表示已有批量上架任务正在运行或取消中。
  batchBusy: boolean;
  // accountName 将账号标识转换为商品页统一的展示名称。
  accountName: (accountID: string) => string;
  // onPreviewImport 将采集成功的商品快照送入现有批量上架预检。
  onPreviewImport: (file: File, targetAccountID: string) => Promise<boolean>;
}

// ItemLinkImportFlow 提供“粘贴分享文本、批量采集、逐条查看、进入预检”的完整入口。
export const ItemLinkImportFlow: React.FC<ItemLinkImportFlowProps> = ({ accounts,preferredAccountID,batchBusy,accountName,onPreviewImport }) => {
  // showModal 表示批量链接采集弹窗是否打开。
  const [showModal, setShowModal] = useState(false);
  // accountID 是读取商品详情与承载新上架商品的自有账号。
  const [accountID, setAccountID] = useState('');
  // sourceText 保存用户按行粘贴的分享文案或商品地址。
  const [sourceText, setSourceText] = useState('');
  // result 保存当前批次的逐条采集结果。
  const [result, setResult] = useState<ItemLinkImportResponse | null>(null);
  // collecting 表示后端正在按顺序采集当前批次。
  const [collecting, setCollecting] = useState(false);
  // previewing 表示成功条目正在进入现有批量预检。
  const [previewing, setPreviewing] = useState(false);
  // formError 是本地参数校验或顶层请求失败的可见说明。
  const [formError, setFormError] = useState('');
  // requestControllerRef 持有当前采集请求的取消器，关闭或重试时中止旧请求。
  const requestControllerRef = useRef<AbortController | null>(null);
  // requestGenerationRef 标识最新采集请求，防止旧响应覆盖新批次。
  const requestGenerationRef = useRef(0);
  // sources 是去除空行后的逐条采集输入。
  const sources = useMemo(/* sourceParser 仅在用户粘贴内容变化时重新分割。 */ () => parseItemLinkSources(sourceText), [sourceText]);
  // successfulRows 是能够进入现有批量上架预检的采集结果。
  const successfulRows = useMemo(
    /* successfulRowsSelector 排除有错误或缺少基础快照的条目。 */ () => result?.rows.filter(/* successfulRowPredicate 判断单条采集结果是否可用。 */ row => !row.error && row.item_id && row.title && row.price && row.images.length > 0) || [],
    [result],
  );
  // unavailableReason 是入口不可用时向用户解释的原因。
  const unavailableReason = accounts.length === 0
    ? '请先添加闲鱼账号'
    : batchBusy
      ? '请先等待当前批量任务结束'
      : '';

  useEffect(/* requestCleanupEffect 为组件实例注册采集请求清理逻辑。 */ () => {
    return /* requestCleanup 在组件卸载时取消未完成的采集请求。 */ () => {
      requestGenerationRef.current += 1;
      requestControllerRef.current?.abort();
    };
  }, []);

  // openModal 按商品页当前上下文选择默认账号并重置上次结果。
  const openModal = () => {
    if (unavailableReason) return;
    // preferredAccount 是账号列表中与页面当前选择一致的账号。
    const preferredAccount = accounts.find(/* preferredAccountPredicate 匹配商品页已选账号。 */ account => account.id === preferredAccountID);
    setAccountID(preferredAccount?.id || accounts[0]?.id || '');
    setSourceText('');
    setResult(null);
    setFormError('');
    setShowModal(true);
  };

  // closeModal 取消当前采集并关闭弹窗，已提交的批量预检仍由现有流程管理。
  const closeModal = () => {
    if (previewing) return;
    requestGenerationRef.current += 1;
    requestControllerRef.current?.abort();
    requestControllerRef.current = null;
    setCollecting(false);
    setShowModal(false);
  };

  // collectLinks 校验用户输入后发起一次最多五十条的顺序采集。
  const collectLinks = async () => {
    if (!accountID) {
      setFormError('请选择用于采集和上架的账号');
      return;
    }
    if (sources.length === 0) {
      setFormError('请粘贴至少一条闲鱼分享文本或商品链接');
      return;
    }
    if (sources.length > MAX_ITEM_LINK_IMPORT_SOURCES) {
      setFormError(`每次最多采集 ${MAX_ITEM_LINK_IMPORT_SOURCES} 条，当前共 ${sources.length} 条`);
      return;
    }
    requestControllerRef.current?.abort();
    // controller 只控制本次采集请求。
    const controller = new AbortController();
    // generation 是本次采集的单调递增代次。
    const generation = ++requestGenerationRef.current;
    requestControllerRef.current = controller;
    setCollecting(true);
    setResult(null);
    setFormError('');
    try {
      // collectedResult 是服务端按输入顺序返回的逐条结果。
      const collectedResult = await collectItemLinks(accountID, sources, { signal: controller.signal });
      if (controller.signal.aborted || generation !== requestGenerationRef.current) return;
      setResult(collectedResult);
    } catch (error: unknown /* collectionError 是批量采集的顶层请求失败。 */) {
      if (controller.signal.aborted || generation !== requestGenerationRef.current) return;
      setFormError(itemErrorMessage(error, '商品链接采集失败，请稍后重试'));
    } finally {
      if (generation === requestGenerationRef.current) {
        setCollecting(false);
        requestControllerRef.current = null;
      }
    }
  };

  // previewCollectedItems 生成不可变 CSV 快照并进入已有的批量预检与确认界面。
  const previewCollectedItems = async () => {
    if (!accountID || successfulRows.length === 0) return;
    // csv 是符合现有批量铺货字段约定的采集商品快照。
    const csv = buildItemLinkImportCSV(successfulRows, accountID);
    // file 是送入批量预检的 UTF-8 CSV 文件。
    const file = new File(['\uFEFF', csv], `闲鱼链接采集-${accountID.slice(0, 8)}.csv`, { type: 'text/csv;charset=utf-8' });
    setPreviewing(true);
    try {
      // opened 表示服务端预检成功且批量上架预览已经打开。
      const opened = await onPreviewImport(file, accountID);
      if (opened) setShowModal(false);
    } finally {
      setPreviewing(false);
    }
  };

  return (
    <>
      <button type="button" onClick={openModal} disabled={Boolean(unavailableReason)} title={unavailableReason || '粘贴分享链接，批量采集后进入上架预检'} className="flex items-center gap-2 rounded-2xl border border-cyan-200 bg-cyan-50 px-5 py-3 font-bold text-cyan-700 shadow-lg shadow-cyan-100 transition-colors hover:border-cyan-300 hover:bg-cyan-100 disabled:cursor-not-allowed disabled:opacity-50">
        <Link2 className="h-4 w-4" />链接采集
      </button>

      {showModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{ maxWidth: '860px' }} role="dialog" aria-modal="true" aria-labelledby="item-link-import-title">
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 id="item-link-import-title" className="text-xl font-extrabold text-gray-900">批量采集闲鱼商品</h3>
                <p className="mt-1 text-xs text-gray-500">每行一条分享文本或商品链接，一次最多 {MAX_ITEM_LINK_IMPORT_SOURCES} 条。</p>
              </div>
              <button type="button" onClick={closeModal} disabled={previewing} className="rounded-xl p-2 transition-colors hover:bg-gray-100 disabled:opacity-50" title="关闭"><X className="h-5 w-5 text-gray-500" /></button>
            </div>

            <div className="modal-body space-y-5">
              <label className="block space-y-2">
                <span className="text-xs font-extrabold tracking-wide text-gray-600">采集与上架账号</span>
                <select aria-label="采集与上架账号" className="ios-input w-full rounded-xl bg-white px-4 py-3 font-bold" value={accountID} disabled={collecting || previewing} onChange={/* accountChangeHandler 切换账号时清空旧采集结果。 */ event => { setAccountID(event.target.value); setResult(null); setFormError(''); }}>
                  {accounts.map(/* accountOptionMapper 渲染用户可以使用的自有账号。 */ account => <option key={account.id} value={account.id}>{accountName(account.id)}</option>)}
                </select>
              </label>

              <label className="block space-y-2">
                <span className="flex items-center justify-between gap-3 text-xs font-extrabold tracking-wide text-gray-600">
                  <span>分享文本或商品链接</span>
                  <span className={sources.length > MAX_ITEM_LINK_IMPORT_SOURCES ? 'text-red-600' : 'text-gray-400'}>{sources.length}/{MAX_ITEM_LINK_IMPORT_SOURCES}</span>
                </span>
                <textarea aria-label="分享文本或商品链接" className="ios-input min-h-40 w-full resize-y rounded-xl px-4 py-3 font-mono text-sm leading-6" value={sourceText} disabled={collecting || previewing} placeholder={'【闲鱼】https://m.tb.cn/... 商品一\nhttps://www.goofish.com/item?id=...'} onChange={/* sourceTextChangeHandler 更新粘贴文本并废弃旧结果。 */ event => { setSourceText(event.target.value); setResult(null); setFormError(''); }} />
              </label>

              {formError && <div role="alert" className="flex items-start gap-2 rounded-xl border border-red-200 bg-red-50 px-4 py-3 text-sm font-bold text-red-700"><AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />{formError}</div>}

              {result && (
                <div className="overflow-hidden rounded-2xl border border-gray-200 bg-white">
                  <div className="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 bg-gray-50 px-4 py-3">
                    <span className="font-extrabold text-gray-900">采集完成：{result.collected} 条成功，{result.failed} 条失败</span>
                    <span className="text-xs font-bold text-gray-500">失败条目不会进入上架预检</span>
                  </div>
                  <div className="max-h-72 divide-y divide-gray-100 overflow-y-auto">
                    {result.rows.map(/* resultRowMapper 渲染单条采集的成功快照或安全失败原因。 */ row => (
                      <div key={row.row_no} className="flex items-start gap-3 px-4 py-3">
                        {row.error ? <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" /> : <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0 text-emerald-600" />}
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-extrabold text-gray-900">{row.error ? `第 ${row.row_no} 条采集失败` : row.title}</div>
                          <div className={`mt-1 text-xs ${row.error ? 'text-amber-700' : 'text-gray-500'}`}>{row.error || `¥${row.price} · ${row.images.length} 张图片 · ID ${row.item_id}`}</div>
                        </div>
                      </div>
                    ))}
                  </div>
                </div>
              )}

              <div className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs leading-5 text-amber-800">
                采集仅读取公开商品的标题、描述、价格和图片；不复制卖家账号、销量、评价或自动发货规则。多规格商品暂不支持，正式上架前仍需在下一步确认。
              </div>
            </div>

            <div className="modal-footer flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
              <button type="button" onClick={closeModal} disabled={previewing} className="rounded-xl bg-gray-100 px-5 py-3 font-bold text-gray-700 hover:bg-gray-200 disabled:opacity-50">取消</button>
              {!result && <button type="button" onClick={/* collectLinksHandler 提交当前批量采集。 */ () => void collectLinks()} disabled={collecting || previewing || sources.length === 0 || sources.length > MAX_ITEM_LINK_IMPORT_SOURCES} className="flex items-center justify-center gap-2 rounded-xl bg-cyan-600 px-6 py-3 font-extrabold text-white hover:bg-cyan-700 disabled:cursor-not-allowed disabled:opacity-50">
                {collecting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Link2 className="h-4 w-4" />}{collecting ? `正在采集 ${sources.length} 条...` : `开始采集 ${sources.length} 条`}
              </button>}
              {result && <button type="button" onClick={/* recollectHandler 清空旧结果以允许用户修改后重新采集。 */ () => { setResult(null); setFormError(''); }} disabled={previewing} className="rounded-xl border border-cyan-200 bg-white px-5 py-3 font-bold text-cyan-700 hover:bg-cyan-50 disabled:opacity-50">修改并重新采集</button>}
              {result && <button type="button" onClick={/* previewImportHandler 将成功采集条目送入现有批量预检。 */ () => void previewCollectedItems()} disabled={previewing || successfulRows.length === 0} className="ios-btn-primary flex items-center justify-center gap-2 rounded-xl px-6 py-3 font-extrabold disabled:cursor-not-allowed disabled:opacity-50">
                <ArrowRight className="h-4 w-4" />{previewing ? '正在生成预览...' : `进入上架预览 ${successfulRows.length} 件`}
              </button>}
            </div>
          </div>
        </div>,
        document.body,
      )}
    </>
  );
};

export default ItemLinkImportFlow;
