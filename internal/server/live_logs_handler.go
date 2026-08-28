package server

import (
	"net/http"
	"strconv"
)

// liveLogLineResponse 是管理端日志接口返回的单行 DTO。
type liveLogLineResponse struct {
	// Sequence 是进程内单调日志游标。
	Sequence uint64 `json:"sequence"`
	// Text 是与容器输出格式一致的日志正文。
	Text string `json:"text"`
}

// liveLogsResponse 是管理端增量轮询使用的有界日志快照 DTO。
type liveLogsResponse struct {
	// Lines 是本次返回的日志行，按写入顺序排列。
	Lines []liveLogLineResponse `json:"lines"`
	// NextCursor 是下次请求的 after 参数值。
	NextCursor uint64 `json:"next_cursor"`
	// OldestCursor 是内存中仍可读取的最旧日志游标。
	OldestCursor uint64 `json:"oldest_cursor"`
}

// liveLogsSnapshot 校验管理端增量游标并返回最多 500 条进程日志。
func (s *Server) liveLogsSnapshot(w http.ResponseWriter, r *http.Request) {
	// after 是客户端已消费的最后一条日志游标。
	after := uint64(0)
	// rawAfter 是请求中尚未通过整数校验的游标文本。
	rawAfter := r.URL.Query().Get("after")
	if rawAfter != "" {
		// parsedAfter 是通过无符号整数校验的游标。
		parsedAfter, parseErr := strconv.ParseUint(rawAfter, 10, 64)
		if parseErr != nil {
			writeErr(w, http.StatusBadRequest, "after 必须是非负整数")
			return
		}
		after = parsedAfter
	}
	// limit 限制单次响应的 DOM 与网络负载。
	limit := 500
	// rawLimit 是请求中尚未通过范围校验的条数文本。
	rawLimit := r.URL.Query().Get("limit")
	if rawLimit != "" {
		// parsedLimit 是经过数值语法校验的单次日志上限。
		parsedLimit, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsedLimit < 1 || parsedLimit > 500 {
			writeErr(w, http.StatusBadRequest, "limit 必须是 1 到 500 的整数")
			return
		}
		limit = parsedLimit
	}
	// snapshot 是进程日志源在当前游标下的一致副本。
	snapshot := s.liveLogs.Snapshot(after, limit)
	// lines 是与底层日志类型解耦后的 HTTP DTO 列表。
	lines := make([]liveLogLineResponse, 0, len(snapshot.Lines))
	// line 表示当前转换为 HTTP 响应的内部日志行。
	for _, line := range snapshot.Lines {
		lines = append(lines, liveLogLineResponse{Sequence: line.Sequence, Text: line.Text})
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, liveLogsResponse{Lines: lines, NextCursor: snapshot.NextCursor, OldestCursor: snapshot.OldestCursor})
}
