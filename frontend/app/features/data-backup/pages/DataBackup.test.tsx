// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

// dataBackupMocks 保存页面测试使用的下载和恢复 API 替身。
const dataBackupMocks = vi.hoisted(/* dataBackupMockFactory 创建跨模块提升的 API 替身。 */ () => ({
  download: vi.fn(),
  restore: vi.fn(),
}));

vi.mock('../api', /* dataBackupAPIMockFactory 隔离页面与真实网络和浏览器下载。 */ () => ({
  downloadDataBackup: dataBackupMocks.download,
  importDataBackup: dataBackupMocks.restore,
}));

import DataBackup from './DataBackup';

describe('DataBackup 页面安全操作', /* dataBackupPageSuite 验证下载入口、恢复确认和重启提示。 */ () => {
  beforeEach(/* dataBackupBeforeEach 为每个场景恢复成功 API 默认值。 */ () => {
    vi.clearAllMocks();
    dataBackupMocks.download.mockResolvedValue(undefined);
    dataBackupMocks.restore.mockResolvedValue({
      filename: 'restore.db',
      size: 4096,
      restartRequired: true,
      message: '备份已校验并暂存，请重启服务完成恢复',
    });
  });

  afterEach(/* dataBackupAfterEach 清理页面 DOM，避免文件输入状态跨测试泄漏。 */ () => cleanup());

  test('下载按钮调用一致性快照接口', /* downloadScenario 验证管理员主动下载流程。 */ async () => {
    render(<DataBackup />);
    fireEvent.click(screen.getByRole('button', { name: '下载数据库备份' }));
    await waitFor(/* downloadAssertion 等待异步下载动作完成。 */ () => expect(dataBackupMocks.download).toHaveBeenCalledTimes(1));
  });

  test('恢复必须选择文件并输入 RESTORE，成功后展示重启提示', /* restoreScenario 验证危险操作的双重确认。 */ async () => {
    render(<DataBackup />);
    // restoreButton 是恢复危险操作按钮，初始必须禁用。
    const restoreButton = screen.getByRole('button', { name: '导入恢复文件' });
    expect(restoreButton.hasAttribute('disabled')).toBe(true);
    // restoreFile 是模拟用户从系统选择器选中的 SQLite 备份。
    const restoreFile = new File(['SQLite fixture'], 'restore.db', { type: 'application/vnd.sqlite3' });
    fireEvent.change(screen.getByLabelText('选择数据库备份文件'), { target: { files: [restoreFile] } });
    fireEvent.change(screen.getByLabelText(/输入/), { target: { value: 'RESTORE' } });
    expect(restoreButton.hasAttribute('disabled')).toBe(false);
    fireEvent.click(restoreButton);
    await waitFor(/* restoreAssertion 等待上传接口收到用户选择的同一文件。 */ () => expect(dataBackupMocks.restore).toHaveBeenCalledWith(restoreFile));
    expect((await screen.findByText('恢复文件已安全暂存')).textContent).toBe('恢复文件已安全暂存');
    expect(screen.getByText(/请重启服务完成恢复/).textContent).toContain('请重启服务完成恢复');
  });
});
