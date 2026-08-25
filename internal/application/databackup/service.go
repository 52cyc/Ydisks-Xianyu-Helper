// Package databackup 提供管理员数据备份下载和恢复暂存用例。
// 本包不感知 HTTP 或数据库方言，数据库快照与文件替换由 Storage 端口实现。
package databackup

import (
	"context"
	"errors"
	"io"
)

const (
	// MaxRestoreBytes 限制单个恢复文件为 512 MiB，避免上传耗尽服务磁盘与内存。
	MaxRestoreBytes int64 = 512 << 20
)

// ErrInvalidUser 表示调用方没有有效的管理员用户标识。
var ErrInvalidUser = errors.New("备份操作用户无效")

// ErrUnsupported 表示当前外置数据库无法使用内置 SQLite 文件级备份。
var ErrUnsupported = errors.New("当前数据库类型暂不支持内置备份，请使用数据库自身的备份工具")

// Artifact 描述一次性数据库快照；调用方读取完成后必须调用 Close 删除临时文件。
type Artifact struct {
	// Reader 提供快照字节流，内容可能包含账号、订单及加密后的秘密数据，禁止记录或缓存。
	Reader io.ReadCloser
	// Filename 是浏览器下载时使用的安全文件名。
	Filename string
	// Size 是快照字节数，用于 HTTP Content-Length。
	Size int64
}

// RestoreResult 描述已完成校验并暂存的恢复文件，不表示数据库已经被替换。
type RestoreResult struct {
	// Filename 是用户上传的原始文件名，仅用于页面确认结果。
	Filename string
	// Size 是已暂存数据库文件的字节数。
	Size int64
	// RestartRequired 表示必须重启服务后才会原子应用恢复文件。
	RestartRequired bool
}

// Storage 定义数据库方言相关的快照与恢复暂存能力。
type Storage interface {
	// CreateSnapshot 创建一致性快照；返回的 Artifact 由调用方关闭。
	CreateSnapshot(context.Context) (Artifact, error)
	// StageRestore 在大小限制内读取、校验并暂存数据库文件，不覆盖运行中的数据库。
	StageRestore(context.Context, io.Reader, string, int64) (RestoreResult, error)
}

// Service 编排管理员身份校验与备份存储端口调用。
type Service struct {
	// storage 负责数据库一致性快照、完整性校验和恢复暂存。
	storage Storage
}

// NewService 构造数据备份应用服务；storage 为空时返回错误，避免运行期延迟失败。
func NewService(storage Storage) (*Service, error) {
	if storage == nil {
		return nil, errors.New("数据备份存储未初始化")
	}
	return &Service{storage: storage}, nil
}

// Export 为有效管理员创建一次性数据库快照；调用方必须关闭返回结果。
func (service *Service) Export(ctx context.Context, userID int64) (Artifact, error) {
	if userID <= 0 {
		return Artifact{}, ErrInvalidUser
	}
	return service.storage.CreateSnapshot(ctx)
}

// Import 校验管理员身份并暂存恢复文件；成功后数据库仍保持不变，重启时才应用。
func (service *Service) Import(ctx context.Context, userID int64, source io.Reader, filename string) (RestoreResult, error) {
	if userID <= 0 {
		return RestoreResult{}, ErrInvalidUser
	}
	if source == nil {
		return RestoreResult{}, errors.New("请选择数据库备份文件")
	}
	return service.storage.StageRestore(ctx, source, filename, MaxRestoreBytes)
}
