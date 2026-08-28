package logging

import (
	"fmt"
	"sync"
	"testing"
)

// TestLiveBufferSnapshotAndEviction 验证增量游标、分段写入和容量淘汰语义。
func TestLiveBufferSnapshotAndEviction(t *testing.T) {
	// buffer 只保留两行，便于验证最旧日志被淘汰后的游标。
	buffer := NewLiveBuffer(2)
	_, _ = buffer.Write([]byte("first"))
	_, _ = buffer.Write([]byte(" line\nsecond\r\nthird\n"))
	// snapshot 是容量淘汰后的全量可见日志。
	snapshot := buffer.Snapshot(0, 500)
	if len(snapshot.Lines) != 2 || snapshot.Lines[0].Text != "second" || snapshot.Lines[1].Text != "third" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if snapshot.OldestCursor != 2 || snapshot.NextCursor != 3 {
		t.Fatalf("cursors=%+v", snapshot)
	}
	// incremental 只应包含客户端已读游标之后的日志。
	incremental := buffer.Snapshot(2, 1)
	if len(incremental.Lines) != 1 || incremental.Lines[0].Sequence != 3 {
		t.Fatalf("incremental=%+v", incremental)
	}
}

// TestLiveBufferConcurrentWrites 验证并发日志写入与快照读取不会丢失完整行。
func TestLiveBufferConcurrentWrites(t *testing.T) {
	// buffer 容量覆盖全部测试日志，避免淘汰干扰并发断言。
	buffer := NewLiveBuffer(100)
	// writers 等待十个并发写入者都完成。
	var writers sync.WaitGroup
	// index 表示当前要启动的并发日志写入者序号。
	for index := 0; index < 10; index++ {
		// index 是当前 goroutine 写入的稳定日志序号。
		index := index
		writers.Add(1)
		go func() {
			defer writers.Done()
			_, _ = buffer.Write([]byte(fmt.Sprintf("line-%d\n", index)))
		}()
	}
	writers.Wait()
	// snapshot 用于确认所有并发输入都形成独立日志行。
	snapshot := buffer.Snapshot(0, 100)
	if len(snapshot.Lines) != 10 {
		t.Fatalf("lines=%d snapshot=%+v", len(snapshot.Lines), snapshot)
	}
}
