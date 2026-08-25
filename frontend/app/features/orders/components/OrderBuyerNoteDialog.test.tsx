// @vitest-environment jsdom
import { cleanup,fireEvent,render,screen,waitFor } from '@testing-library/react';
import { afterEach,beforeEach,describe,expect,test,vi } from 'vitest';
import type { Order } from '../api';
import { getOrderBuyerNote,saveOrderBuyerNote } from '../api';
import { OrderBuyerNoteDialog } from './OrderBuyerNoteDialog';

vi.mock('../api', /* orderBuyerNoteApiMockFactory 提供备注弹窗读取和保存的可控请求。 */ () => ({
  getOrderBuyerNote: vi.fn(),
  saveOrderBuyerNote: vi.fn(),
}));

// getBuyerNoteMock 是当前买家备注读取请求的测试替身。
const getBuyerNoteMock = vi.mocked(getOrderBuyerNote);
// saveBuyerNoteMock 是当前买家备注保存请求的测试替身。
const saveBuyerNoteMock = vi.mocked(saveOrderBuyerNote);
// orderFixture 是备注弹窗定位账号和买家的最小订单。
const orderFixture = {
  id: 'order-1',order_id: 'order-1',cookie_id: 'account-1',item_id: 'item-1',buyer_id: 'buyer-1',buyer_name: '测试买家',quantity: 1,amount: '2.00',status: 'pending_ship',
} as Order;

describe('OrderBuyerNoteDialog', /* 当前回调验证订单页买家备注的读取、编辑、保存和关闭。 */ () => {
  beforeEach(/* 当前回调重置备注 API 替身并配置默认成功结果。 */ () => {
    vi.clearAllMocks();
    getBuyerNoteMock.mockResolvedValue({ account_id: 'account-1',buyer_id: 'buyer-1',content: '老备注',updated_at: 1 });
    saveBuyerNoteMock.mockResolvedValue({ account_id: 'account-1',buyer_id: 'buyer-1',content: '新备注',updated_at: 2 });
  });

  afterEach(/* 当前回调清理备注弹窗 DOM。 */ () => cleanup());

  test('读取已有备注并保存新内容', /* 当前回调验证备注表单使用订单账号和买家作为隔离键。 */ async () => {
    // closeDialog 记录测试中的弹窗关闭动作。
    const closeDialog = vi.fn();
    render(<OrderBuyerNoteDialog order={orderFixture} onClose={closeDialog} />);
    await waitFor(/* noteLoadedAssertion 等待已保存备注显示。 */ () => expect(screen.getByText('老备注')).toBeTruthy());
    expect(getBuyerNoteMock).toHaveBeenCalledWith('account-1','buyer-1',expect.objectContaining({ signal: expect.any(AbortSignal) }));
    fireEvent.click(screen.getByText('编辑备注'));
    fireEvent.change(screen.getByLabelText('备注内容'),{ target: { value: '新备注' } });
    fireEvent.click(screen.getByText('保存备注'));
    await waitFor(/* noteSavedAssertion 等待保存结果回到只读态。 */ () => expect(screen.getByText('新备注')).toBeTruthy());
    expect(saveBuyerNoteMock).toHaveBeenCalledWith('account-1','buyer-1','新备注',expect.objectContaining({ signal: expect.any(AbortSignal) }));
    fireEvent.click(screen.getByRole('button',{ name: '关闭订单备注' }));
    expect(closeDialog).toHaveBeenCalledTimes(1);
  });

  test('读取失败时展示可访问的错误提示', /* 当前回调验证备注请求失败不会留下无反馈弹窗。 */ async () => {
    getBuyerNoteMock.mockRejectedValueOnce(new Error('备注服务不可用'));
    render(<OrderBuyerNoteDialog order={orderFixture} onClose={vi.fn()} />);
    await waitFor(/* errorAssertion 等待错误文本进入 alert 区域。 */ () => expect(screen.getByRole('alert').textContent).toContain('备注服务不可用'));
  });
});
