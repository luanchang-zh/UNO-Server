package logx

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// TestWithContext_IncludesTraceID 验证 WithContext 日志携带 trace_id。
func TestWithContext_IncludesTraceID(t *testing.T) {
	var buffer bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger := NewFromSlog(base)

	ctx := IntoContext(context.Background(), logger, "trace-abc")
	logger.WithContext(ctx).Info("hello", "k", "v")

	line := buffer.String()
	if !strings.Contains(line, `"trace_id":"trace-abc"`) {
		t.Fatalf("日志应包含 trace_id，实际: %s", line)
	}
	if !strings.Contains(line, `"msg":"hello"`) {
		t.Fatalf("日志应包含消息，实际: %s", line)
	}

	var payload map[string]any
	if err := json.Unmarshal(buffer.Bytes(), &payload); err != nil {
		t.Fatalf("解析 JSON 日志失败: %v", err)
	}
	if payload["level"] != "INFO" {
		t.Fatalf("期望 INFO，实际 %v", payload["level"])
	}
}

// TestNewTraceID_Length 验证生成的 trace_id 长度。
func TestNewTraceID_Length(t *testing.T) {
	traceID := NewTraceID()
	if len(traceID) != 32 {
		t.Fatalf("期望 32 位 hex，实际 len=%d value=%s", len(traceID), traceID)
	}
}
