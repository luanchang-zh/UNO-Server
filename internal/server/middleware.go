package server

import (
	"net/http"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
)

// statusRecorder 捕获写入的 HTTP 状态码与响应体大小，供访问日志使用。
type statusRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
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

// withAccessLog 为每个 HTTP 请求注入 trace_id，并在请求结束时只打一条访问日志。
func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		startedAt := time.Now()
		traceID := logx.FromRequestTrace(request.Header.Get(logx.HeaderTraceID))
		ctx := logx.IntoContext(request.Context(), s.logger, traceID)
		request = request.WithContext(ctx)

		writer.Header().Set(logx.HeaderTraceID, traceID)
		recorder := &statusRecorder{ResponseWriter: writer, statusCode: http.StatusOK}

		next.ServeHTTP(recorder, request)

		duration := time.Since(startedAt)
		fields := []any{
			"method", request.Method,
			"path", request.URL.Path,
			"status", recorder.statusCode,
			"duration_ms", duration.Milliseconds(),
			"bytes", recorder.bytesWritten,
			"remote", request.RemoteAddr,
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
