package logging

import (
	"bytes"
	"sync"
)

// LiveLine 表示管理端可读取的一条已格式化运行日志。
type LiveLine struct {
	// Sequence 是进程内单调递增的日志游标，重启后从 1 重新开始。
	Sequence uint64
	// Text 是经现有 slog 格式化和脱敏策略处理后的完整单行文本。
	Text string
}

// LiveSnapshot 是一次增量日志读取结果。
type LiveSnapshot struct {
	// Lines 包含游标之后的有界日志行。
	Lines []LiveLine
	// NextCursor 是客户端下次增量读取应携带的游标。
	NextCursor uint64
	// OldestCursor 是当前内存中仍可读取的最旧日志游标。
	OldestCursor uint64
}

// LiveBuffer 在内存中保留有界的最新日志行；mu 保护全部字段，Write 与 Snapshot 可并发调用。
type LiveBuffer struct {
	// mu 保护日志行、未完整行和递增游标。
	mu sync.RWMutex
	// capacity 是内存中最多保留的完整日志行数。
	capacity int
	// lines 按写入顺序保存尚未被淘汰的日志。
	lines []LiveLine
	// pending 保存跨 Write 调用但尚未以换行结束的字节。
	pending []byte
	// nextSequence 是下一条完整日志要分配的游标。
	nextSequence uint64
}

// NewLiveBuffer 创建指定行容量的进程内日志缓冲区；非正容量使用 2000 行。
func NewLiveBuffer(capacity int) *LiveBuffer {
	if capacity <= 0 {
		capacity = 2000
	}
	return &LiveBuffer{capacity: capacity, nextSequence: 1}
}

// Write 实现 io.Writer，将 slog 已编码的输出按换行切分并追加到有界缓冲区。
func (b *LiveBuffer) Write(data []byte) (int, error) {
	// written 保留 io.Writer 契约要求的原始输入字节数。
	written := len(data)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending = append(b.pending, data...)
	for {
		// newline 是当前未处理字节中第一个换行符的位置。
		newline := bytes.IndexByte(b.pending, '\n')
		if newline < 0 {
			break
		}
		// line 是不含换行符的一条完整日志副本。
		line := string(bytes.TrimSuffix(b.pending[:newline], []byte{'\r'}))
		b.pending = b.pending[newline+1:]
		if line == "" {
			continue
		}
		b.lines = append(b.lines, LiveLine{Sequence: b.nextSequence, Text: line})
		b.nextSequence++
		if len(b.lines) > b.capacity {
			b.lines = append([]LiveLine(nil), b.lines[len(b.lines)-b.capacity:]...)
		}
	}
	return written, nil
}

// Snapshot 返回 after 游标之后最多 limit 条日志；limit 非正或超过 500 时使用 500。
func (b *LiveBuffer) Snapshot(after uint64, limit int) LiveSnapshot {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	// oldest 是当前环形保留范围内的最旧游标。
	oldest := b.nextSequence
	if len(b.lines) > 0 {
		oldest = b.lines[0].Sequence
	}
	// result 保存需要交付给调用方的独立日志副本。
	result := make([]LiveLine, 0, limit)
	// line 表示当前检查是否新于客户端游标的日志行。
	for _, line := range b.lines {
		if line.Sequence <= after {
			continue
		}
		result = append(result, line)
		if len(result) == limit {
			break
		}
	}
	// next 默认延续调用方游标，有新数据时指向本次最后一条。
	next := after
	if len(result) > 0 {
		next = result[len(result)-1].Sequence
	} else if after < oldest-1 {
		next = oldest - 1
	}
	return LiveSnapshot{Lines: result, NextCursor: next, OldestCursor: oldest}
}
