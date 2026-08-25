package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// pendingRestoreSuffix 标识已校验、等待下次进程启动应用的 SQLite 数据库。
	pendingRestoreSuffix = ".restore-pending"
)

// ErrBackupUnsupported 表示当前数据库不是本地 SQLite 文件，无法使用内置文件级备份。
var ErrBackupUnsupported = errors.New("当前数据库类型暂不支持内置备份，请使用数据库自身的备份工具")

// SQLiteBackupFile 描述临时一致性快照，调用方读取完成后必须删除 Path。
type SQLiteBackupFile struct {
	// Path 是仅供本机请求生命周期读取的受限权限临时文件。
	Path string
	// Size 是快照字节数。
	Size int64
}

// SQLiteRestoreStage 描述已完成完整性与结构校验的待恢复文件。
type SQLiteRestoreStage struct {
	// Size 是暂存文件字节数。
	Size int64
}

// PendingSQLiteRestore 保存启动期数据库切换及失败回滚所需路径。
type PendingSQLiteRestore struct {
	// targetPath 是服务配置的 SQLite 主数据库路径。
	targetPath string
	// safetyPath 是替换前数据库的自动安全副本；成功启动后仍保留供人工回退。
	safetyPath string
	// movedCompanions 保存已移动的 WAL/SHM 原路径和安全副本路径。
	movedCompanions [][2]string
	// hadTarget 表示恢复前是否存在主数据库。
	hadTarget bool
}

// SQLitePathFromURL 从兼容数据库地址中提取本地 SQLite 文件路径；非 SQLite 返回 false。
func SQLitePathFromURL(raw string) (string, bool) {
	// value 是去除首尾空白后的数据库地址，禁止把空值解释为当前目录。
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}
	if !strings.Contains(value, "://") {
		return value, true
	}
	// scheme、path 是数据库协议和协议后的本地路径。
	scheme, path, _ := strings.Cut(value, "://")
	if scheme != "sqlite" && scheme != "sqlite3" {
		return "", false
	}
	return path, strings.TrimSpace(path) != ""
}

// CreateSQLiteBackup 使用 SQLite VACUUM INTO 生成包含 WAL 已提交内容的一致性快照。
func CreateSQLiteBackup(ctx context.Context, database *sql.DB, databasePath string) (SQLiteBackupFile, error) {
	if database == nil || strings.TrimSpace(databasePath) == "" {
		return SQLiteBackupFile{}, ErrBackupUnsupported
	}
	// parent 是快照所在目录，与数据库同卷便于可靠创建受限临时文件。
	parent := filepath.Dir(databasePath)
	// placeholder 只用于获得随机且当前不存在的快照路径，SQLite 要求目标文件不存在。
	placeholder, createErr := os.CreateTemp(parent, ".ydisks-backup-*.db")
	if createErr != nil {
		return SQLiteBackupFile{}, fmt.Errorf("创建备份临时文件失败: %w", createErr)
	}
	// snapshotPath 在本次请求结束时由 Artifact.Close 删除，错误路径在本函数内删除。
	snapshotPath := placeholder.Name()
	if closeErr := placeholder.Close(); closeErr != nil { // closeErr 表示随机占位文件句柄关闭失败。
		_ = os.Remove(snapshotPath)
		return SQLiteBackupFile{}, fmt.Errorf("关闭备份临时文件失败: %w", closeErr)
	}
	if removeErr := os.Remove(snapshotPath); removeErr != nil { // removeErr 表示为 SQLite 清空快照目标路径失败。
		return SQLiteBackupFile{}, fmt.Errorf("准备备份临时路径失败: %w", removeErr)
	}
	// quotedPath 是 SQLite 字符串字面量形式的本机路径，仅来源于 CreateTemp，不接收外部输入。
	quotedPath := strings.ReplaceAll(snapshotPath, "'", "''")
	if _, vacuumErr := database.ExecContext(ctx, "VACUUM INTO '"+quotedPath+"'"); vacuumErr != nil { // vacuumErr 表示一致性快照生成失败。
		_ = os.Remove(snapshotPath)
		return SQLiteBackupFile{}, fmt.Errorf("创建数据库一致性快照失败: %w", vacuumErr)
	}
	if chmodErr := os.Chmod(snapshotPath, 0o600); chmodErr != nil { // chmodErr 表示快照权限收紧失败。
		_ = os.Remove(snapshotPath)
		return SQLiteBackupFile{}, fmt.Errorf("限制备份文件权限失败: %w", chmodErr)
	}
	// info 提供下载响应的精确文件大小。
	info, statErr := os.Stat(snapshotPath)
	if statErr != nil {
		_ = os.Remove(snapshotPath)
		return SQLiteBackupFile{}, fmt.Errorf("读取备份文件信息失败: %w", statErr)
	}
	return SQLiteBackupFile{Path: snapshotPath, Size: info.Size()}, nil
}

// StageSQLiteRestore 将上传流写入受限临时文件，完成 SQLite 完整性和核心表校验后原子设为待恢复文件。
func StageSQLiteRestore(ctx context.Context, databasePath string, source io.Reader, maxBytes int64) (SQLiteRestoreStage, error) {
	if strings.TrimSpace(databasePath) == "" {
		return SQLiteRestoreStage{}, ErrBackupUnsupported
	}
	if source == nil || maxBytes <= 0 {
		return SQLiteRestoreStage{}, errors.New("恢复文件无效")
	}
	// parent 是数据库目录，保证暂存和启动期替换位于同一文件系统。
	parent := filepath.Dir(databasePath)
	// temporary 是上传期间的受限文件，校验失败必须删除。
	temporary, createErr := os.CreateTemp(parent, ".ydisks-restore-upload-*.db")
	if createErr != nil {
		return SQLiteRestoreStage{}, fmt.Errorf("创建恢复暂存文件失败: %w", createErr)
	}
	// temporaryPath 在成功 rename 前仍由本函数负责清理。
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	// written 是最多读取上限加一字节后的实际大小，用于可靠识别超限文件。
	written, copyErr := io.Copy(temporary, io.LimitReader(source, maxBytes+1))
	if copyErr != nil {
		_ = temporary.Close()
		return SQLiteRestoreStage{}, fmt.Errorf("读取恢复文件失败: %w", copyErr)
	}
	if syncErr := temporary.Sync(); syncErr != nil { // syncErr 表示上传内容持久化到暂存文件失败。
		_ = temporary.Close()
		return SQLiteRestoreStage{}, fmt.Errorf("写入恢复文件失败: %w", syncErr)
	}
	if closeErr := temporary.Close(); closeErr != nil { // closeErr 表示恢复暂存文件关闭失败。
		return SQLiteRestoreStage{}, fmt.Errorf("关闭恢复文件失败: %w", closeErr)
	}
	if written == 0 {
		return SQLiteRestoreStage{}, errors.New("恢复文件为空")
	}
	if written > maxBytes {
		return SQLiteRestoreStage{}, fmt.Errorf("恢复文件不能超过 %d MiB", maxBytes>>20)
	}
	if validateErr := validateSQLiteRestore(ctx, temporaryPath); validateErr != nil { // validateErr 表示 SQLite 完整性或应用结构不符合要求。
		return SQLiteRestoreStage{}, validateErr
	}
	// pendingPath 是进程下次启动前唯一识别的恢复候选文件；新上传会安全替换旧候选。
	pendingPath := databasePath + pendingRestoreSuffix
	if renameErr := os.Rename(temporaryPath, pendingPath); renameErr != nil { // renameErr 表示暂存文件原子发布失败。
		return SQLiteRestoreStage{}, fmt.Errorf("保存待恢复文件失败: %w", renameErr)
	}
	return SQLiteRestoreStage{Size: written}, nil
}

// validateSQLiteRestore 以只读方式验证上传文件是完整且包含应用核心表的 SQLite 数据库。
func validateSQLiteRestore(ctx context.Context, path string) error {
	// candidate 是仅在当前校验函数中存在的只读连接，不执行迁移或写入。
	candidate, openErr := sql.Open(string(driverSQLite), "file:"+path+"?mode=ro&_pragma=busy_timeout(5000)")
	if openErr != nil {
		return errors.New("恢复文件不是有效的 SQLite 数据库")
	}
	defer candidate.Close()
	// integrity 保存 SQLite 全库一致性检查结果，只有精确 ok 才允许暂存。
	var integrity string
	if queryErr := candidate.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); queryErr != nil || integrity != "ok" { // queryErr 表示全库一致性检查无法完成。
		return errors.New("恢复文件完整性校验失败")
	}
	// requiredTable 是识别本应用数据库所需的核心表；缺少任一表都拒绝恢复。
	for _, requiredTable := range []string{"users", "goose_db_version"} {
		// count 是 sqlite_master 中匹配核心表的数量。
		var count int
		if queryErr := candidate.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", requiredTable).Scan(&count); queryErr != nil || count != 1 { // queryErr 表示核心表结构查询失败。
			return errors.New("恢复文件不是 Ydisks 数据库备份")
		}
	}
	return nil
}

// ApplyPendingSQLiteRestore 在数据库打开前原子切换已暂存文件，并返回可在启动失败时回滚的句柄。
func ApplyPendingSQLiteRestore(databasePath string, now time.Time) (*PendingSQLiteRestore, error) {
	if strings.TrimSpace(databasePath) == "" {
		return nil, nil
	}
	// pendingPath 是导入接口已经完成校验的候选数据库。
	pendingPath := databasePath + pendingRestoreSuffix
	if _, statErr := os.Stat(pendingPath); errors.Is(statErr, os.ErrNotExist) { // statErr 表示待恢复文件当前不存在。
		return nil, nil
	} else if statErr != nil {
		return nil, fmt.Errorf("检查待恢复数据库失败: %w", statErr)
	}
	// safetyPath 保留替换前完整数据库，时间戳避免覆盖历史安全副本。
	safetyPath := fmt.Sprintf("%s.before-restore-%s", databasePath, now.Format("20060102-150405"))
	// restore 记录主文件和伴随文件移动，用于数据库打开失败时恢复原状态。
	restore := &PendingSQLiteRestore{targetPath: databasePath, safetyPath: safetyPath}
	if _, statErr := os.Stat(databasePath); statErr == nil { // statErr 表示当前主数据库存在并可读取元数据。
		restore.hadTarget = true
		if renameErr := os.Rename(databasePath, safetyPath); renameErr != nil { // renameErr 表示主数据库安全副本切换失败。
			return nil, fmt.Errorf("保存恢复前安全副本失败: %w", renameErr)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("检查当前数据库失败: %w", statErr)
	}
	// suffix 是 SQLite 可能遗留的伴随文件；必须隔离，避免旧 WAL 被应用到恢复后的数据库。
	for _, suffix := range []string{"-wal", "-shm"} {
		// sourcePath、backupPath 是伴随文件的当前路径和安全副本路径。
		sourcePath, backupPath := databasePath+suffix, safetyPath+suffix
		if _, statErr := os.Stat(sourcePath); statErr == nil { // statErr 表示当前伴随文件存在并需要隔离。
			if renameErr := os.Rename(sourcePath, backupPath); renameErr != nil { // renameErr 表示 WAL 或 SHM 安全隔离失败。
				_ = restore.Rollback()
				return nil, fmt.Errorf("隔离 SQLite 伴随文件失败: %w", renameErr)
			}
			restore.movedCompanions = append(restore.movedCompanions, [2]string{sourcePath, backupPath})
		}
	}
	if renameErr := os.Rename(pendingPath, databasePath); renameErr != nil { // renameErr 表示待恢复数据库原子切换失败。
		_ = restore.Rollback()
		return nil, fmt.Errorf("应用待恢复数据库失败: %w", renameErr)
	}
	return restore, nil
}

// Rollback 在新数据库无法打开或迁移时恢复原主文件与 WAL/SHM；可重复调用。
func (restore *PendingSQLiteRestore) Rollback() error {
	if restore == nil {
		return nil
	}
	// failures 收集所有文件恢复错误，避免首个失败阻断其他伴随文件回滚。
	var failures []error
	if removeErr := os.Remove(restore.targetPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) { // removeErr 表示移除启动失败的新数据库失败。
		failures = append(failures, removeErr)
	}
	if restore.hadTarget {
		if renameErr := os.Rename(restore.safetyPath, restore.targetPath); renameErr != nil { // renameErr 表示恢复原主数据库失败。
			failures = append(failures, renameErr)
		}
	}
	// companion 是需要从安全路径移回原路径的 WAL 或 SHM 文件对。
	for _, companion := range restore.movedCompanions {
		if renameErr := os.Rename(companion[1], companion[0]); renameErr != nil && !errors.Is(renameErr, os.ErrNotExist) { // renameErr 表示恢复原 WAL 或 SHM 失败。
			failures = append(failures, renameErr)
		}
	}
	return errors.Join(failures...)
}

// SafetyPath 返回成功恢复前自动保留的数据库副本路径，不包含数据内容。
func (restore *PendingSQLiteRestore) SafetyPath() string {
	if restore == nil || !restore.hadTarget {
		return ""
	}
	return restore.safetyPath
}
