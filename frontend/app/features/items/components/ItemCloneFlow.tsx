import { AlertTriangle,ArrowRight,CheckSquare,Copy,Search,UserRound,X } from 'lucide-react';
import React,{ useMemo,useState } from 'react';
import { createPortal } from 'react-dom';
import { buildItemCloneCSV,getItemCloneEligibility,itemCloneKey } from '../cloneState';
import type { AccountDetail,Item } from '../models';

// ItemCloneFlowProps 描述账号间商品克隆入口依赖的账号、商品和预检动作。
interface ItemCloneFlowProps {
  // accounts 是用户可选择的全部闲鱼账号。
  accounts: AccountDetail[];
  // items 是已同步到本地的全部商品。
  items: Item[];
  // preferredSourceAccountID 是列表筛选或同步框提供的优先源账号。
  preferredSourceAccountID: string;
  // batchBusy 表示已有批量任务正在运行或取消中。
  batchBusy: boolean;
  // accountName 将账号标识转换为当前页面统一的展示名称。
  accountName: (accountID: string) => string;
  // onPreviewClone 将生成的商品快照提交到现有批量预检流程。
  onPreviewClone: (file: File, targetAccountID: string) => Promise<boolean>;
}

// formatClonePrice 将商品价格转换为克隆列表中的紧凑展示文本。
const formatClonePrice = (price?: string): string => {
  // value 是去除首尾空白后的原始价格。
  const value = String(price || '').trim();
  if (!value) return '价格未知';
  return /^[¥￥]/.test(value) ? value : `¥${value}`;
};

// ItemCloneFlow 提供“源账号、目标账号、商品勾选、预检执行”的完整克隆入口。
export const ItemCloneFlow: React.FC<ItemCloneFlowProps> = ({ accounts,items,preferredSourceAccountID,batchBusy,accountName,onPreviewClone }) => {
  // showModal 表示账号间克隆弹窗是否打开。
  const [showModal, setShowModal] = useState(false);
  // sourceAccountID 是当前单选的源账号。
  const [sourceAccountID, setSourceAccountID] = useState('');
  // targetAccountID 是当前单选的目标账号。
  const [targetAccountID, setTargetAccountID] = useState('');
  // selectedKeys 保存用户勾选的源商品联合键。
  const [selectedKeys, setSelectedKeys] = useState<Set<string>>(() => new Set());
  // searchKeyword 保存源商品标题或 ID 搜索词。
  const [searchKeyword, setSearchKeyword] = useState('');
  // previewing 表示克隆快照正在提交服务端预检。
  const [previewing, setPreviewing] = useState(false);
  // sourceAccountIDs 是至少拥有一件已同步商品的源账号标识集合。
  const sourceAccountIDs = useMemo(
    /* sourceAccountSelector 只保留拥有本地商品的账号。 */ () => new Set(items.map(/* sourceIDMapper 读取商品所属账号。 */ item => item.cookie_id)),
    [items],
  );
  // sourceAccounts 是可作为源账号的账号列表。
  const sourceAccounts = useMemo(
    /* sourceAccountsSelector 按本地商品归属筛选账号。 */ () => accounts.filter(/* sourceAccountPredicate 判断账号是否拥有已同步商品。 */ account => sourceAccountIDs.has(account.id)),
    [accounts, sourceAccountIDs],
  );
  // targetAccounts 是排除当前源账号后的目标账号列表。
  const targetAccounts = useMemo(
    /* targetAccountsSelector 自动排除源账号。 */ () => accounts.filter(/* targetAccountPredicate 判断账号能否作为当前目标。 */ account => account.id !== sourceAccountID),
    [accounts, sourceAccountID],
  );
  // sourceItems 是当前源账号已同步的商品列表。
  const sourceItems = useMemo(
    /* sourceItemsSelector 读取当前源账号商品。 */ () => items.filter(/* sourceItemPredicate 判断商品是否属于当前源账号。 */ item => item.cookie_id === sourceAccountID),
    [items, sourceAccountID],
  );
  // visibleSourceItems 是经标题或商品 ID 过滤后的源商品列表。
  const visibleSourceItems = useMemo(/* visibleSourceItemsSelector 按标题或商品 ID 过滤源商品。 */ () => {
    // keyword 是用于不区分大小写匹配的搜索词。
    const keyword = searchKeyword.trim().toLowerCase();
    if (!keyword) return sourceItems;
    return sourceItems.filter(/* sourceSearchPredicate 匹配商品标题或平台商品 ID。 */ item => (
      String(item.item_title || '').toLowerCase().includes(keyword)
      || String(item.item_id || '').toLowerCase().includes(keyword)
    ));
  }, [searchKeyword, sourceItems]);
  // selectableVisibleItems 是当前搜索结果中满足第一版克隆条件的商品。
  const selectableVisibleItems = useMemo(
    /* selectableItemsSelector 排除缺字段或多规格商品。 */ () => visibleSourceItems.filter(/* eligibleItemPredicate 判断商品能否进入克隆预检。 */ item => getItemCloneEligibility(item).eligible),
    [visibleSourceItems],
  );
  // selectedItems 是源账号中当前已勾选且仍满足克隆条件的商品。
  const selectedItems = useMemo(
    /* selectedItemsSelector 从源账号商品恢复有序选择结果。 */ () => sourceItems.filter(/* selectedItemPredicate 判断商品是否被勾选且可克隆。 */ item => selectedKeys.has(itemCloneKey(item)) && getItemCloneEligibility(item).eligible),
    [selectedKeys, sourceItems],
  );
  // allVisibleSelected 表示当前可选搜索结果是否已经全部勾选。
  const allVisibleSelected = selectableVisibleItems.length > 0 && selectableVisibleItems.every(/* selectedPredicate 判断可选商品是否已被勾选。 */ item => selectedKeys.has(itemCloneKey(item)));
  // unavailableReason 是入口不可用时向用户解释的原因。
  const unavailableReason = accounts.length < 2
    ? '至少需要两个账号'
    : sourceAccounts.length === 0
      ? '请先同步源账号商品'
      : batchBusy
        ? '请先等待当前批量任务结束'
        : '';

  // openModal 初始化符合当前页面上下文的源账号和目标账号。
  const openModal = () => {
    if (unavailableReason) return;
    // preferredSource 是优先使用的列表筛选账号或同步账号。
    const preferredSource = sourceAccounts.find(/* preferredSourcePredicate 查找用户当前关注的源账号。 */ account => account.id === preferredSourceAccountID)?.id;
    // nextSource 是本次弹窗最终使用的源账号。
    const nextSource = preferredSource || sourceAccounts[0]?.id || '';
    // nextTarget 是自动排除源账号后的首个目标账号。
    const nextTarget = accounts.find(/* nextTargetPredicate 选择不同于源账号的首个目标账号。 */ account => account.id !== nextSource)?.id || '';
    setSourceAccountID(nextSource);
    setTargetAccountID(nextTarget);
    setSelectedKeys(new Set());
    setSearchKeyword('');
    setShowModal(true);
  };

  // closeModal 在没有提交请求时关闭克隆弹窗。
  const closeModal = () => {
    if (!previewing) setShowModal(false);
  };

  // changeSourceAccount 切换源账号并重新选择一个不同的目标账号。
  const changeSourceAccount = (nextSourceAccountID: string) => {
    setSourceAccountID(nextSourceAccountID);
    setSelectedKeys(new Set());
    setSearchKeyword('');
    if (targetAccountID === nextSourceAccountID) {
      // nextTargetAccountID 是切换源账号后首个不相同的账号。
      const nextTargetAccountID = accounts.find(/* nextTargetPredicate 排除新源账号。 */ account => account.id !== nextSourceAccountID)?.id || '';
      setTargetAccountID(nextTargetAccountID);
    }
  };

  // toggleItemSelection 切换单件源商品的勾选状态。
  const toggleItemSelection = (item: Item) => {
    if (!getItemCloneEligibility(item).eligible) return;
    // key 是当前商品的账号与商品联合键。
    const key = itemCloneKey(item);
    setSelectedKeys(/* selectionUpdater 基于上一选择集合切换当前商品。 */ previous => {
      // next 是避免原地修改 React 状态的新选择集合。
      const next = new Set(previous);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  // toggleVisibleSelection 批量全选或取消当前搜索结果中的可克隆商品。
  const toggleVisibleSelection = () => {
    setSelectedKeys(/* visibleSelectionUpdater 只修改当前搜索结果中的可选商品。 */ previous => {
      // next 是避免原地修改 React 状态的新选择集合。
      const next = new Set(previous);
      selectableVisibleItems.forEach(/* visibleItemUpdater 更新当前可选商品的勾选状态。 */ item => {
        // key 是当前可选商品的联合键。
        const key = itemCloneKey(item);
        if (allVisibleSelected) next.delete(key);
        else next.add(key);
      });
      return next;
    });
  };

  // previewClone 生成不可变商品快照并进入已有的批量预检界面。
  const previewClone = async () => {
    if (!targetAccountID || selectedItems.length === 0) return;
    // csv 是符合现有批量铺货接口字段约定的商品快照。
    const csv = buildItemCloneCSV(selectedItems, targetAccountID);
    // file 是传给批量预检接口的 UTF-8 CSV 文件。
    const file = new File(['\uFEFF', csv], `商品克隆-${sourceAccountID.slice(0, 6)}-到-${targetAccountID.slice(0, 6)}.csv`, { type: 'text/csv;charset=utf-8' });
    setPreviewing(true);
    try {
      // opened 表示服务端预检成功且批量预览界面已经打开。
      const opened = await onPreviewClone(file, targetAccountID);
      if (opened) setShowModal(false);
    } finally {
      setPreviewing(false);
    }
  };

  return (
    <>
      <button
        type="button"
        onClick={openModal}
        disabled={Boolean(unavailableReason)}
        title={unavailableReason || '从一个账号选择商品，发布到另一个账号'}
        className="flex items-center gap-2 rounded-2xl border border-violet-200 bg-violet-50 px-5 py-3 font-bold text-violet-700 shadow-lg shadow-violet-100 transition-colors hover:border-violet-300 hover:bg-violet-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-violet-500 focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
      >
        <Copy className="h-4 w-4" />
        账号间克隆
      </button>

      {showModal && createPortal(
        <div className="modal-overlay-centered">
          <div className="modal-container" style={{ maxWidth: '920px' }} role="dialog" aria-modal="true" aria-labelledby="item-clone-title">
            <div className="modal-header flex items-center justify-between gap-4">
              <div>
                <h3 id="item-clone-title" className="text-xl font-extrabold text-gray-900">账号间克隆商品</h3>
                <p className="mt-1 text-xs text-gray-500">选择源账号和目标账号，再勾选要克隆的商品。</p>
              </div>
              <button type="button" onClick={closeModal} disabled={previewing} className="rounded-xl p-2 transition-colors hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-violet-500 disabled:opacity-50" title="关闭">
                <X className="h-5 w-5 text-gray-500" />
              </button>
            </div>

            <div className="modal-body space-y-5">
              <div className="relative overflow-hidden rounded-2xl border border-violet-100 bg-gradient-to-r from-slate-50 via-violet-50 to-emerald-50 p-4 sm:p-5">
                <div className="grid grid-cols-1 items-end gap-3 sm:grid-cols-[1fr_auto_1fr]">
                  <label className="space-y-2">
                    <span className="flex items-center gap-2 text-xs font-extrabold tracking-wide text-slate-600"><UserRound className="h-4 w-4 text-violet-600" />源账号</span>
                    <select aria-label="源账号" className="ios-input w-full rounded-xl bg-white px-4 py-3 font-bold" value={sourceAccountID} onChange={/* sourceChangeHandler 响应源账号切换。 */ event => changeSourceAccount(event.target.value)}>
                      {sourceAccounts.map(/* sourceOptionMapper 渲染可用源账号选项。 */ account => <option key={account.id} value={account.id}>{accountName(account.id)}</option>)}
                    </select>
                  </label>
                  <div className="flex items-center justify-center pb-3 text-violet-500" aria-hidden="true">
                    <div className="hidden h-px w-5 bg-violet-300 sm:block" />
                    <span className="mx-2 rounded-full border border-violet-200 bg-white p-2 shadow-sm"><ArrowRight className="h-4 w-4 motion-reduce:transform-none" /></span>
                    <div className="hidden h-px w-5 bg-violet-300 sm:block" />
                  </div>
                  <label className="space-y-2">
                    <span className="flex items-center gap-2 text-xs font-extrabold tracking-wide text-slate-600"><UserRound className="h-4 w-4 text-emerald-600" />目标账号</span>
                    <select aria-label="目标账号" className="ios-input w-full rounded-xl bg-white px-4 py-3 font-bold" value={targetAccountID} onChange={/* targetChangeHandler 响应目标账号切换。 */ event => setTargetAccountID(event.target.value)}>
                      {targetAccounts.map(/* targetOptionMapper 渲染自动排除源账号后的目标选项。 */ account => <option key={account.id} value={account.id}>{accountName(account.id)}</option>)}
                    </select>
                  </label>
                </div>
              </div>

              <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <div className="font-extrabold text-gray-900">选择要克隆的商品</div>
                  <p className="mt-1 text-xs text-gray-500">已选 {selectedItems.length} 件，共 {sourceItems.length} 件；克隆前还会逐条预检。</p>
                </div>
                <label className="relative min-w-0 sm:w-72">
                  <span className="sr-only">搜索源商品</span>
                  <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
                  <input className="ios-input w-full rounded-xl py-2.5 pl-10 pr-3" placeholder="搜索标题或商品 ID" value={searchKeyword} onChange={/* searchChangeHandler 更新商品搜索词。 */ event => setSearchKeyword(event.target.value)} />
                </label>
              </div>

              <div className="overflow-hidden rounded-2xl border border-gray-200 bg-white">
                <div className="flex items-center justify-between border-b border-gray-100 bg-gray-50 px-4 py-3">
                  <button type="button" onClick={toggleVisibleSelection} disabled={selectableVisibleItems.length === 0} className="flex items-center gap-2 text-sm font-extrabold text-violet-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-violet-500 disabled:text-gray-400">
                    <CheckSquare className="h-4 w-4" />{allVisibleSelected ? '取消当前全选' : '全选当前可克隆商品'}
                  </button>
                  <span className="text-xs font-bold text-gray-400">库存按 1、邮费按包邮生成，可在预览中复核</span>
                </div>
                <div className="max-h-[380px] divide-y divide-gray-100 overflow-y-auto">
                  {visibleSourceItems.map(/* cloneItemMapper 渲染源账号商品选择行。 */ item => {
                    // eligibility 是当前商品的克隆资格和禁用原因。
                    const eligibility = getItemCloneEligibility(item);
                    // key 是当前商品在多账号场景中的稳定联合键。
                    const key = itemCloneKey(item);
                    // checked 表示当前商品是否已经被勾选。
                    const checked = selectedKeys.has(key);
                    return (
                      <label key={key} className={`flex gap-3 px-4 py-3 transition-colors motion-reduce:transition-none ${eligibility.eligible ? 'cursor-pointer hover:bg-violet-50/50' : 'cursor-not-allowed bg-gray-50/70 opacity-70'}`}>
                        <input type="checkbox" className="mt-5 h-4 w-4 rounded border-gray-300 text-violet-600 focus:ring-violet-500" checked={checked} disabled={!eligibility.eligible} onChange={/* itemSelectionHandler 切换当前源商品勾选状态。 */ () => toggleItemSelection(item)} />
                        <div className="h-14 w-14 shrink-0 overflow-hidden rounded-xl border border-gray-100 bg-gray-100">
                          {item.item_image ? <img src={item.item_image} alt="" className="h-full w-full object-cover" /> : <div className="flex h-full w-full items-center justify-center text-gray-300"><Copy className="h-5 w-5" /></div>}
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-sm font-extrabold text-gray-900">{item.item_title || '未命名商品'}</div>
                          <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500">
                            <span className="font-bold text-violet-700">{formatClonePrice(item.item_price)}</span>
                            <span className="rounded bg-gray-100 px-1.5 py-0.5">ID: {item.item_id}</span>
                            {!eligibility.eligible && <span className="inline-flex items-center gap-1 font-bold text-amber-700"><AlertTriangle className="h-3.5 w-3.5" />{eligibility.reason}</span>}
                          </div>
                        </div>
                      </label>
                    );
                  })}
                  {visibleSourceItems.length === 0 && <div className="px-4 py-14 text-center text-sm text-gray-400">没有匹配的源商品</div>}
                </div>
              </div>

              <div className="rounded-xl border border-amber-200 bg-amber-50 px-4 py-3 text-xs leading-5 text-amber-800">
                第一版克隆标题、描述、价格和主图，不复制自动发货规则；多规格商品暂不支持。发布前请在下一步预览中确认类目和内容。
              </div>
            </div>

            <div className="modal-footer flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
              <button type="button" onClick={closeModal} disabled={previewing} className="rounded-xl bg-gray-100 px-5 py-3 font-bold text-gray-700 transition-colors hover:bg-gray-200 disabled:opacity-50">取消</button>
              <button type="button" onClick={/* previewCloneHandler 提交勾选商品进入批量预检。 */ () => void previewClone()} disabled={previewing || !targetAccountID || selectedItems.length === 0} className="ios-btn-primary flex items-center justify-center gap-2 rounded-xl px-6 py-3 font-extrabold disabled:cursor-not-allowed disabled:opacity-50">
                <ArrowRight className="h-4 w-4" />{previewing ? '正在生成预览...' : `预览并克隆 ${selectedItems.length} 件商品`}
              </button>
            </div>
          </div>
        </div>,
        document.body,
      )}
    </>
  );
};

export default ItemCloneFlow;
