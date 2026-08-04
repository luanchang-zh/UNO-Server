// Package logx 提供带 context 的结构化日志（Info/Warn/Error）与 trace_id 透传。
//
// 约定：业务路径不在中间层刷访问日志；HTTP 请求 / WS 包处理结束时各打一条。
package logx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
)

type contextKey int

const (
	// HeaderTraceID 为 HTTP 传入/回传 trace 的标准头。
	HeaderTraceID = "X-Trace-Id"

	loggerKey contextKey = iota
	traceIDKey
)

// Logger 是对 slog 的薄封装，支持 WithContext 链式调用。
type Logger struct {
	base *slog.Logger
}

// New 使用 JSON Handler 创建根 Logger；level 为最低输出级别。
func New(level slog.Level) *Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return &Logger{base: slog.New(handler)}
}

// NewFromSlog 包装已有 slog.Logger（测试或自定义 Handler 时使用）。
func NewFromSlog(base *slog.Logger) *Logger {
	if base == nil {
		base = slog.Default()
	}
	return &Logger{base: base}
}

// With 返回附加固定字段的子 Logger。
func (l *Logger) With(args ...any) *Logger {
	return &Logger{base: l.base.With(args...)}
}

// WithContext 从 ctx 取出 trace_id 等字段，返回可直接 Info/Warn/Error 的上下文日志器。
func (l *Logger) WithContext(ctx context.Context) *ContextLogger {
	if l == nil {
		l = NewFromSlog(nil)
	}
	base := l.base
	if ctxLogger, ok := ctx.Value(loggerKey).(*Logger); ok && ctxLogger != nil {
		base = ctxLogger.base
	}
	if traceID := TraceID(ctx); traceID != "" {
		base = base.With("trace_id", traceID)
	}
	return &ContextLogger{base: base, ctx: ctx}
}

// ContextLogger 绑定了 context 与 trace 字段，供请求/消息结束时打点。
type ContextLogger struct {
	base *slog.Logger
	ctx  context.Context
}

// Info 输出 Info 级别日志。
func (c *ContextLogger) Info(msg string, args ...any) {
	c.base.InfoContext(c.ctx, msg, args...)
}

// Warn 输出 Warn 级别日志。
func (c *ContextLogger) Warn(msg string, args ...any) {
	c.base.WarnContext(c.ctx, msg, args...)
}

// Error 输出 Error 级别日志。
func (c *ContextLogger) Error(msg string, args ...any) {
	c.base.ErrorContext(c.ctx, msg, args...)
}

// IntoContext 将 Logger 与 trace_id 写入 context，供下游 WithContext 使用。
func IntoContext(ctx context.Context, logger *Logger, traceID string) context.Context {
	if logger == nil {
		logger = NewFromSlog(nil)
	}
	if traceID == "" {
		traceID = NewTraceID()
	}
	ctx = context.WithValue(ctx, loggerKey, logger)
	ctx = context.WithValue(ctx, traceIDKey, traceID)
	return ctx
}

// TraceID 从 context 读取 trace_id；不存在则返回空串。
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if value, ok := ctx.Value(traceIDKey).(string); ok {
		return value
	}
	return ""
}

// NewTraceID 生成 16 字节随机 trace_id（hex）。
func NewTraceID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		// 极端情况下退回时间相关占位，避免空 trace 阻断请求。
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(buffer)
}

// FromRequestTrace 优先使用客户端传入的 trace，否则新建。
func FromRequestTrace(incoming string) string {
	if incoming != "" {
		return incoming
	}
	return NewTraceID()
}
