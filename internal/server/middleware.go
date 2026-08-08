package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
)

// statusRecorder 捕获写入的 HTTP 状态码与响应体大小，供访问日志使用。
// 实现 http.Hijacker，以便 WebSocket 升级能劫持底层连接。
type statusRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
	hijacked     bool
}

// WriteHeader 记录状态码后转交底层 ResponseWriter。
func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

// Write 累计写出字节数；若尚未显式 WriteHeader，按 200 计。
func (r *statusRecorder) Write(payload []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(payload)
	r.bytesWritten += n
	return n, err
}

// Hijack 将连接控制权交给 WebSocket 升级逻辑。
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	r.hijacked = true
	// 升级成功后 gorilla 会写 101；此处标记为 Switching Protocols 便于访问日志。
	if r.statusCode == 0 {
		r.statusCode = http.StatusSwitchingProtocols
	}
	return hijacker.Hijack()
}

// withAccessLog 为每个 HTTP 请求注入 trace_id，并在请求结束时只打一条访问日志。
func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		traceID := logx.FromRequestTrace(request.Header.Get(logx.HeaderTraceID))
		ctx := logx.IntoContext(request.Context(), s.logger, traceID)
		request = request.WithContext(ctx)

		writer.Header().Set(logx.HeaderTraceID, traceID)
		recorder := &statusRecorder{ResponseWriter: writer, statusCode: 0}

		next.ServeHTTP(recorder, request)

		// 未写状态码且未劫持时，默认 200（部分 handler 只 Write body）。
		if recorder.statusCode == 0 && !recorder.hijacked {
			recorder.statusCode = http.StatusOK
		}

		duration := time.Since(startedAt)
		route := metricRoute(request.Pattern)
		if s.metrics != nil {
			s.metrics.ObserveHTTPRequest(request.Method, route, recorder.statusCode, duration)
		}
		fields := []any{
			"event", "http_request",
			"method", request.Method,
			"route", route,
			"path", request.URL.Path,
			"status_code", recorder.statusCode,
			"duration_ms", duration.Milliseconds(),
			"response_bytes", recorder.bytesWritten,
			"remote_addr", request.RemoteAddr,
		}
		if recorder.hijacked {
			fields = append(fields, "hijacked", true)
		}

		contextLogger := s.logger.WithContext(ctx)
		switch {
		case recorder.statusCode >= 500:
			contextLogger.Error("http request", fields...)
		case recorder.statusCode >= 400:
			contextLogger.Warn("http request", fields...)
		default:
			contextLogger.Info("http request", fields...)
		}
	})
}

// metricRoute 从 ServeMux 的固定模式中移除方法前缀，并折叠未匹配路径。
func metricRoute(pattern string) string {
	if separator := strings.IndexByte(pattern, ' '); separator >= 0 {
		pattern = pattern[separator+1:]
	}
	if pattern == "" {
		return "unmatched"
	}
	return pattern
}
