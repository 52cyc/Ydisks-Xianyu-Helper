package db

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSQLiteBackupAndRestoreLifecycle 验证在线快照一致、导入先暂存且启动切换保留安全副本。
func TestSQLiteBackupAndRestoreLifecycle(t *testing.T) {
	// ctx 限制数据库备份和校验操作在测试生命周期内完成。
	ctx := context.Background()
	// targetPath 是当前运行数据库文件，restoreSourcePath 是模拟用户导入的旧快照。
	targetPath := filepath.Join(t.TempDir(), "current.db")
	// current、dialect、openErr 分别是迁移后的测试数据库、方言和打开错误。
	current, dialect, openErr := Open(ctx, targetPath)
	if openErr != nil {
		t.Fatalf("打开当前数据库失败: %v", openErr)
	}
	if dialect != DialectSQLite {
		t.Fatalf("测试数据库方言=%s", dialect)
	}
	defer current.Close()
	// insertErr 表示写入快照前标记设置失败。
	if _, insertErr := current.ExecContext(ctx, "INSERT INTO system_settings(key, value) VALUES(?, ?)", "backup_marker", "before"); insertErr != nil {
		t.Fatalf("写入快照标记失败: %v", insertErr)
	}
	// snapshot、snapshotErr 分别是一致性快照及创建错误。
	snapshot, snapshotErr := CreateSQLiteBackup(ctx, current, targetPath)
	if snapshotErr != nil {
		t.Fatalf("创建快照失败: %v", snapshotErr)
	}
	defer os.Remove(snapshot.Path)
	if snapshot.Size <= 0 {
		t.Fatalf("快照大小=%d", snapshot.Size)
	}
	// updateErr 表示在线数据库在快照后继续写入失败。
	if _, updateErr := current.ExecContext(ctx, "UPDATE system_settings SET value=? WHERE key=?", "after", "backup_marker"); updateErr != nil {
		t.Fatalf("更新当前数据库失败: %v", updateErr)
	}
	// snapshotFile 是模拟浏览器重新上传的快照文件流。
	snapshotFile, fileErr := os.Open(snapshot.Path)
	if fileErr != nil {
		t.Fatalf("打开快照文件失败: %v", fileErr)
	}
	// stage、stageErr 分别是完整性校验后的暂存结果及错误。
	stage, stageErr := StageSQLiteRestore(ctx, targetPath, snapshotFile, 512<<20)
	_ = snapshotFile.Close()
	if stageErr != nil {
		t.Fatalf("暂存恢复文件失败: %v", stageErr)
	}
	if stage.Size != snapshot.Size {
		t.Fatalf("暂存大小=%d want=%d", stage.Size, snapshot.Size)
	}
	// currentValue 是重启前仍由当前连接读取到的新值，证明导入未在线覆盖。
	var currentValue string
	if queryErr := current.QueryRowContext(ctx, "SELECT value FROM system_settings WHERE key=?", "backup_marker").Scan(&currentValue); queryErr != nil || currentValue != "after" { // queryErr 表示重启前标记读取失败。
		t.Fatalf("重启前当前值=%q err=%v", currentValue, queryErr)
	}
	if closeErr := current.Close(); closeErr != nil { // closeErr 表示模拟重启前连接池关闭失败。
		t.Fatalf("关闭当前数据库失败: %v", closeErr)
	}
	// restore、applyErr 分别是启动期文件切换句柄及错误。
	restore, applyErr := ApplyPendingSQLiteRestore(targetPath, time.Date(2026, 8, 24, 12, 0, 0, 0, time.Local))
	if applyErr != nil {
		t.Fatalf("应用恢复文件失败: %v", applyErr)
	}
	if restore == nil || restore.SafetyPath() == "" {
		t.Fatal("恢复后未保留安全副本")
	}
	// restored 是模拟真实启动重新打开并迁移后的恢复数据库。
	restored, _, restoredErr := Open(ctx, targetPath)
	if restoredErr != nil {
		t.Fatalf("打开恢复数据库失败: %v", restoredErr)
	}
	defer restored.Close()
	// restoredValue 是恢复后应回到快照时点的设置值。
	var restoredValue string
	if queryErr := restored.QueryRowContext(ctx, "SELECT value FROM system_settings WHERE key=?", "backup_marker").Scan(&restoredValue); queryErr != nil || restoredValue != "before" { // queryErr 表示恢复后标记读取失败。
		t.Fatalf("恢复后值=%q err=%v", restoredValue, queryErr)
	}
}

// TestStageSQLiteRestoreRejectsInvalidFile 验证任意文本不能成为待恢复数据库。
func TestStageSQLiteRestoreRejectsInvalidFile(t *testing.T) {
	// targetPath 是当前数据库位置，函数只会在其旁边创建并清理上传临时文件。
	targetPath := filepath.Join(t.TempDir(), "current.db")
	// stageErr 是无效文本载荷的校验错误。
	_, stageErr := StageSQLiteRestore(context.Background(), targetPath, strings.NewReader("not a sqlite database"), 1024)
	if stageErr == nil {
		t.Fatal("无效文件未被拒绝")
	}
	if _, statErr := os.Stat(targetPath + pendingRestoreSuffix); !os.IsNotExist(statErr) { // statErr 表示无效上传后仍存在待恢复文件。
		t.Fatalf("无效文件产生了待恢复文件: %v", statErr)
	}
}

// TestSQLitePathFromURL 验证本地兼容路径与外置数据库地址的识别边界。
func TestSQLitePathFromURL(t *testing.T) {
	// cases 是数据库地址、预期路径和 SQLite 判定组成的测试表。
	cases := []struct {
		// raw 是待解析数据库地址。
		raw string
		// path 是期望提取的本地文件路径。
		path string
		// ok 表示是否属于 SQLite。
		ok bool
	}{
		{raw: "/tmp/app.db", path: "/tmp/app.db", ok: true},
		{raw: "sqlite://data/app.db", path: "data/app.db", ok: true},
		{raw: "mysql://user:pass@tcp(localhost)/app", ok: false},
	}
	for _, testCase := range cases { // testCase 是当前数据库地址识别样例。
		// path、ok 是当前地址的实际解析结果。
		path, ok := SQLitePathFromURL(testCase.raw)
		if path != testCase.path || ok != testCase.ok {
			t.Fatalf("SQLitePathFromURL(%q)=(%q,%t)", testCase.raw, path, ok)
		}
	}
}
