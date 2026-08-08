package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/auth"
	"github.com/luanchang-zh/UNO-Server/internal/config"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
)

// TestHandleGuestLogin_Success 验证游客登录接口返回 player_id 与 token。
func TestHandleGuestLogin_Success(t *testing.T) {
	srv := newTestServer(io.Discard)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", bytes.NewBufferString(`{"nickname":"Alice"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	srv.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get(logx.HeaderTraceID) == "" {
		t.Fatal("响应应回写 X-Trace-Id")
	}

	var response guestLoginResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if response.PlayerID == 0 || response.Token == "" || response.Nickname != "Alice" {
		t.Fatalf("响应字段不完整: %+v", response)
	}
}

// TestHandleGuestLogin_EmptyBody 验证空 body 也能登录并使用默认昵称。
func TestHandleGuestLogin_EmptyBody(t *testing.T) {
	srv := newTestServer(io.Discard)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/guest", http.NoBody)
	recorder := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}

	var response guestLoginResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if response.Nickname != "游客" {
		t.Fatalf("期望默认昵称 游客，实际 %q", response.Nickname)
	}
}

// TestHandleGuestLogin_InvalidNickname 验证超长昵称返回 400。
func TestHandleGuestLogin_InvalidNickname(t *testing.T) {
	srv := newTestServer(io.Discard)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/guest",
		bytes.NewBufferString(`{"nickname":"一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十多余字"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("期望状态码 400，实际 %d，body=%s", recorder.Code, recorder.Body.String())
	}
}

// TestHandleHealthz 验证健康检查接口。
func TestHandleHealthz(t *testing.T) {
	srv := newTestServer(io.Discard)

	request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	recorder := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("期望状态码 200，实际 %d", recorder.Code)
	}
}

// TestAccessLog_OneLineWithTraceID 验证每个请求结束只打一条日志且含 trace_id。
func TestAccessLog_OneLineWithTraceID(t *testing.T) {
	var logBuffer bytes.Buffer
	srv := newTestServer(&logBuffer)

	request := httptest.NewRequest(http.MethodGet, "/healthz", http.NoBody)
	request.Header.Set(logx.HeaderTraceID, "client-trace-001")
	recorder := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Header().Get(logx.HeaderTraceID) != "client-trace-001" {
		t.Fatalf("应回传客户端 trace，实际 %q", recorder.Header().Get(logx.HeaderTraceID))
	}

	// 去掉可能的尾部空行后应恰好一行访问日志。
	lines := nonEmptyLines(logBuffer.String())
	if len(lines) != 1 {
		t.Fatalf("期望 1 条访问日志，实际 %d: %q", len(lines), logBuffer.String())
	}
	if !strings.Contains(lines[0], `"trace_id":"client-trace-001"`) {
		t.Fatalf("日志应含 trace_id，实际: %s", lines[0])
	}
	if !strings.Contains(lines[0], `"msg":"http request"`) {
		t.Fatalf("日志应为 http request，实际: %s", lines[0])
	}
	if !strings.Contains(lines[0], `"status":200`) {
		t.Fatalf("日志应含 status=200，实际: %s", lines[0])
	}
}

// TestAccessLog_WarnOn4xx 验证 4xx 使用 Warn 级别。
func TestAccessLog_WarnOn4xx(t *testing.T) {
	var logBuffer bytes.Buffer
	srv := newTestServer(&logBuffer)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/guest",
		bytes.NewBufferString(`{"nickname":"一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十多余字"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d", recorder.Code)
	}
	lines := nonEmptyLines(logBuffer.String())
	if len(lines) != 1 {
		t.Fatalf("期望 1 条日志，实际 %d", len(lines))
	}
	if !strings.Contains(lines[0], `"level":"WARN"`) {
		t.Fatalf("4xx 应为 WARN，实际: %s", lines[0])
	}
}

// newTestServer 构造带可捕获日志输出的测试 Server。
func newTestServer(logOutput io.Writer) *Server {
	cfg := config.Config{
		HTTPAddr:        ":0",
		ReadTimeout:     2 * time.Second,
		WriteTimeout:    2 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		TokenTTL:        time.Hour,
		MaxNicknameLen:  32,
	}
	authService := auth.NewService(auth.Options{
		TokenTTL:       cfg.TokenTTL,
		MaxNicknameLen: cfg.MaxNicknameLen,
	})
	base := slog.New(slog.NewJSONHandler(logOutput, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return New(cfg, authService, logx.NewFromSlog(base), Dependencies{})
}

// nonEmptyLines 按行拆分并去掉空行。
func nonEmptyLines(text string) []string {
	rawLines := strings.Split(strings.TrimSpace(text), "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
