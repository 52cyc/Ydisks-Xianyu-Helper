package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	databackupapp "xianyu-go/internal/application/databackup"
	"xianyu-go/internal/db"
)

// dataBackupStorage 将应用层备份端口适配到 SQLite 一致性快照与恢复暂存实现。
type dataBackupStorage struct {
	// store 提供当前数据库连接和方言；只在 adapter 内部进入 db 基础设施函数。
	store *db.Store
	// databasePath 是本地 SQLite 主文件路径；外置数据库场景为空。
	databasePath string
}

// temporaryArtifactReader 在响应关闭后同时关闭并删除含敏感数据的临时快照。
type temporaryArtifactReader struct {
	// file 是 HTTP 下载期间读取的快照文件。
	file *os.File
	// path 是 Close 时必须删除的临时路径。
	path string
}

// NewDataBackupStorage 构造数据库备份适配器；非 SQLite 仍返回端口，并在调用时给出明确不支持错误。
func NewDataBackupStorage(store *db.Store, databasePath string) databackupapp.Storage {
	return &dataBackupStorage{store: store, databasePath: databasePath}
}

// CreateSnapshot 创建 SQLite 一致性临时快照并把清理责任封装到返回的 Reader。
func (storage *dataBackupStorage) CreateSnapshot(ctx context.Context) (databackupapp.Artifact, error) {
	if storage == nil || storage.store == nil || storage.store.Dialect != db.DialectSQLite {
		return databackupapp.Artifact{}, databackupapp.ErrUnsupported
	}
	// snapshot 是 db 层生成的一致性临时文件。
	snapshot, snapshotErr := db.CreateSQLiteBackup(ctx, storage.store.DB, storage.databasePath)
	if snapshotErr != nil {
		if errors.Is(snapshotErr, db.ErrBackupUnsupported) {
			return databackupapp.Artifact{}, databackupapp.ErrUnsupported
		}
		return databackupapp.Artifact{}, snapshotErr
	}
	// file 是交给 HTTP 层顺序读取的受限快照句柄。
	file, openErr := os.Open(snapshot.Path)
	if openErr != nil {
		_ = os.Remove(snapshot.Path)
		return databackupapp.Artifact{}, fmt.Errorf("打开备份快照失败: %w", openErr)
	}
	// filename 只含固定前缀和本机时间，不暴露真实数据库路径。
	filename := "ydisks-backup-" + time.Now().Format("20060102-150405") + ".db"
	return databackupapp.Artifact{Reader: &temporaryArtifactReader{file: file, path: snapshot.Path}, Filename: filename, Size: snapshot.Size}, nil
}

// StageRestore 校验上传内容并暂存为下次重启应用的 SQLite 数据库。
func (storage *dataBackupStorage) StageRestore(ctx context.Context, source io.Reader, filename string, maxBytes int64) (databackupapp.RestoreResult, error) {
	if storage == nil || storage.store == nil || storage.store.Dialect != db.DialectSQLite {
		return databackupapp.RestoreResult{}, databackupapp.ErrUnsupported
	}
	// stage 是 db 层完成完整性与核心结构校验后的暂存结果。
	stage, stageErr := db.StageSQLiteRestore(ctx, storage.databasePath, source, maxBytes)
	if stageErr != nil {
		if errors.Is(stageErr, db.ErrBackupUnsupported) {
			return databackupapp.RestoreResult{}, databackupapp.ErrUnsupported
		}
		return databackupapp.RestoreResult{}, stageErr
	}
	return databackupapp.RestoreResult{Filename: filepath.Base(filename), Size: stage.Size, RestartRequired: true}, nil
}

// Read 从临时快照读取下载数据，读取内容不得进入日志。
func (reader *temporaryArtifactReader) Read(buffer []byte) (int, error) {
	return reader.file.Read(buffer)
}

// Close 关闭快照句柄并删除磁盘临时文件；删除失败不会把敏感路径返回给客户端。
func (reader *temporaryArtifactReader) Close() error {
	if reader == nil {
		return nil
	}
	// closeErr、removeErr 分别保存句柄关闭和临时文件删除结果。
	closeErr := reader.file.Close()
	// removeErr 表示含敏感数据的临时快照删除失败。
	removeErr := os.Remove(reader.path)
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return removeErr
	}
	return closeErr
}
