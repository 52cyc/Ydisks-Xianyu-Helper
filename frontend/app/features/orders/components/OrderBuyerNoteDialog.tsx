import { FilePenLine,LoaderCircle,X } from 'lucide-react';
import React,{ useEffect,useRef,useState } from 'react';
import type { Order } from '../api';
import { getOrderBuyerNote,saveOrderBuyerNote } from '../api';

/** OrderBuyerNoteDialogProps 描述订单页买家备注弹窗的当前订单和关闭动作。 */
export interface OrderBuyerNoteDialogProps {
  // order 是备注按账号和买家隔离时的定位依据。
  order: Order;
  // onClose 在用户放弃或完成备注后关闭弹窗。
  onClose: () => void;
}

/** OrderBuyerNoteDialog 读取并编辑当前订单买家的共享运营备注。 */
export const OrderBuyerNoteDialog: React.FC<OrderBuyerNoteDialogProps> = ({ order,onClose }) => {
  // content 保存服务端已持久化的备注文本。
  const [content,setContent] = useState('');
  // draft 是当前可编辑但尚未保存的表单文本。
  const [draft,setDraft] = useState('');
  // loading 表示弹窗正在读取当前买家备注。
  const [loading,setLoading] = useState(true);
  // saving 防止用户重复提交同一份备注。
  const [saving,setSaving] = useState(false);
  // editing 控制弹窗展示只读内容还是编辑表单。
  const [editing,setEditing] = useState(false);
  // error 保存备注读取或保存失败时的用户可见说明。
  const [error,setError] = useState('');
  // saveController 持有保存请求的取消控制器，弹窗卸载时不允许旧响应更新状态。
  const saveController = useRef<AbortController | null>(null);

  useEffect(/* 当订单账号或买家改变时重新读取备注，cleanup 取消不再有效的请求。 */ () => {
    // controller 隔离当前订单备注的读取生命周期。
    const controller = new AbortController();
    setLoading(true);
    setError('');
    void getOrderBuyerNote(order.cookie_id,order.buyer_id,{ signal: controller.signal }).then(
      /* note 是服务端返回的当前买家完整备注。 */ note => {
        if (controller.signal.aborted) return;
        setContent(note.content);
        setDraft(note.content);
      },
      /* loadError 是非主动取消时需要告知用户的读取失败。 */ loadError => {
        if (!controller.signal.aborted) setError(loadError instanceof Error ? loadError.message : '读取订单备注失败');
      },
    ).finally(/* 当前请求未取消时结束加载态。 */ () => {
      if (!controller.signal.aborted) setLoading(false);
    });
    return /* 订单切换或弹窗关闭时取消读取请求。 */ () => controller.abort();
  },[order.buyer_id,order.cookie_id]);

  useEffect(/* 弹窗卸载时取消尚未完成的保存请求。 */ () => () => saveController.current?.abort(),[]);

  /** saveNote 将当前草稿保存到账号与买家共享的备注记录。 */
  const saveNote = async (): Promise<void> => {
    saveController.current?.abort();
    // controller 隔离本次保存请求，防止关闭弹窗后更新页面。
    const controller = new AbortController();
    saveController.current = controller;
    setSaving(true);
    setError('');
    try {
      // note 是服务端规范化并持久化后的最终备注。
      const note = await saveOrderBuyerNote(order.cookie_id,order.buyer_id,draft,{ signal: controller.signal });
      if (controller.signal.aborted) return;
      setContent(note.content);
      setDraft(note.content);
      setEditing(false);
    } catch (saveError /* saveError 是备注保存失败的原因。 */) {
      if (!controller.signal.aborted) setError(saveError instanceof Error ? saveError.message : '保存订单备注失败');
    } finally {
      if (!controller.signal.aborted) setSaving(false);
    }
  };

  /** cancelEditing 放弃未保存草稿并恢复最近一次服务端内容。 */
  const cancelEditing = (): void => {
    setDraft(content);
    setEditing(false);
    setError('');
  };

  return <div className="modal-overlay-centered" onClick={/* 点击遮罩关闭备注弹窗。 */ onClose}>
    <div role="dialog" aria-modal="true" aria-labelledby="order-buyer-note-title" className="modal-container max-w-xl" onClick={/* 阻止弹窗内交互触发遮罩关闭。 */ event => event.stopPropagation()}>
      <div className="modal-header">
        <div className="flex min-w-0 items-center gap-3"><span className="rounded-xl bg-sky-50 p-2 text-sky-600"><FilePenLine className="h-5 w-5" /></span><div className="min-w-0"><h3 id="order-buyer-note-title" className="text-xl font-extrabold text-slate-900">订单备注</h3><p className="mt-1 truncate text-xs text-slate-500">{order.buyer_name || order.buyer_id} · 同账号下该买家共享</p></div></div>
        <button type="button" onClick={onClose} className="rounded-full bg-slate-100 p-2 text-slate-500 transition hover:bg-slate-200" aria-label="关闭订单备注"><X className="h-5 w-5" /></button>
      </div>
      <div className="modal-body space-y-4">
        {error && <div role="alert" className="rounded-xl bg-red-50 px-4 py-3 text-sm text-red-700">{error}</div>}
        {loading ? <div role="status" className="flex min-h-40 items-center justify-center gap-2 text-sm text-slate-500"><LoaderCircle className="h-5 w-5 animate-spin" />正在读取备注…</div> : editing ? <div><label htmlFor="order-buyer-note" className="mb-2 block text-sm font-bold text-slate-700">备注内容</label><textarea id="order-buyer-note" autoFocus value={draft} onChange={/* 输入时更新尚未保存的备注草稿。 */ event => setDraft(event.target.value)} maxLength={2000} rows={9} placeholder="记录沟通偏好、充值账号注意事项或售后情况…" className="ios-input min-h-52 w-full resize-y rounded-xl px-4 py-3 text-sm leading-6" /><div className="mt-1 text-right text-xs text-slate-400">{draft.length}/2000</div></div> : <div className="min-h-40 whitespace-pre-wrap break-words rounded-xl border border-slate-100 bg-slate-50 px-4 py-3 text-sm leading-6 text-slate-700">{content || '尚未添加备注。'}</div>}
      </div>
      <div className="modal-footer">
        {editing ? <div className="flex w-full justify-end gap-2"><button type="button" onClick={cancelEditing} disabled={saving} className="rounded-xl px-4 py-2.5 text-sm font-bold text-slate-600 transition hover:bg-slate-100 disabled:opacity-50">取消</button><button type="button" onClick={/* 点击保存时提交当前备注草稿。 */ () => void saveNote()} disabled={saving} className="ios-btn-primary rounded-xl px-5 py-2.5 text-sm font-bold disabled:opacity-50">{saving ? '保存中…' : '保存备注'}</button></div> : <div className="flex w-full justify-end gap-2"><button type="button" onClick={onClose} className="rounded-xl px-4 py-2.5 text-sm font-bold text-slate-600 transition hover:bg-slate-100">关闭</button><button type="button" onClick={/* 切换到备注编辑态并保留当前内容。 */ () => setEditing(true)} disabled={loading} className="ios-btn-primary rounded-xl px-5 py-2.5 text-sm font-bold disabled:opacity-50">编辑备注</button></div>}
      </div>
    </div>
  </div>;
};
