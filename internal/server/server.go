// Package server 提供 HTTP 服务装配与生命周期管理。
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/auth"
	"github.com/luanchang-zh/UNO-Server/internal/config"
	"github.com/luanchang-zh/UNO-Server/internal/idgen"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/room"
	"github.com/luanchang-zh/UNO-Server/internal/session"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// Dependencies 汇总 HTTP Server 之外由进程入口装配的业务依赖。
type Dependencies struct {
	// IDGenerator 与鉴权服务共享节点生成器，避免业务主键碰撞。
	IDGenerator idgen.Source
	// MatchRepository 非空时记录开局和终局结算。
	MatchRepository store.MatchRepository
}

// Server 封装标准库 HTTP 服务。
type Server struct {
	cfg        config.Config
	auth       *auth.Service
	sessions   *session.Manager
	rooms      *room.Manager
	httpServer *http.Server
	logger     *logx.Logger
}

// New 根据配置和进程依赖创建 HTTP Server 并注册路由。
func New(cfg config.Config, authService *auth.Service, logger *logx.Logger, dependencies Dependencies) *Server {
	if logger == nil {
		logger = logx.NewFromSlog(nil)
	}

	mux := http.NewServeMux()
	srv := &Server{
		cfg:      cfg,
		auth:     authService,
		sessions: session.NewManager(logger),
		rooms: room.NewManager(logger, room.Options{
			TurnTimeout:        cfg.TurnTimeout,
			ManagedActionDelay: cfg.ManagedActionDelay,
			TimeoutStrikeLimit: cfg.TimeoutStrikeLimit,
			IDGenerator:        dependencies.IDGenerator,
			MatchRepository:    dependencies.MatchRepository,
			PersistenceTimeout: cfg.MySQLOperationTimeout,
		}),
		logger: logger,
	}

	mux.HandleFunc("GET /healthz", srv.handleHealthz)
	mux.HandleFunc("POST /api/v1/auth/guest", srv.handleGuestLogin)
	mux.HandleFunc("GET /ws", srv.handleWebSocket)

	// 访问日志中间件包在最外层：每个 HTTP 请求结束只打一条日志（WS 握手返回即记一条）。
	// WebSocket 为长连接：不设 WriteTimeout/ReadTimeout，仅限制读 header。
	srv.httpServer = &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.withAccessLog(mux),
		ReadHeaderTimeout: cfg.ReadTimeout,
	}

	return srv
}

// Start 开始监听；阻塞直到服务关闭或发生不可恢复错误。
func (s *Server) Start() error {
	s.logger.WithContext(context.Background()).Info("HTTP 服务启动", "addr", s.cfg.HTTPAddr)
	err := s.httpServer.ListenAndServe()
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("listen and serve: %w", err)
}

// Shutdown 先断开全部 WebSocket，再在超时时间内优雅关闭 HTTP。
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.WithContext(ctx).Info("HTTP 服务关闭中", "ws_connections", s.sessions.Count())
	// 主动关闭在线连接，触发各自 Close → 断线日志 + Unregister。
	s.sessions.CloseAll()
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	return nil
}

// handleHealthz 提供存活探针（不在 handler 内打访问日志，由中间件统一处理）。
func (s *Server) handleHealthz(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// guestLoginRequest 为游客登录请求体。
type guestLoginRequest struct {
	Nickname string `json:"nickname"`
}

// guestLoginResponse 为游客登录成功响应体。
type guestLoginResponse struct {
	PlayerID  int64  `json:"player_id"`
	Nickname  string `json:"nickname"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// handleGuestLogin 处理游客登录：创建玩家身份并返回 token。
// 错误用 errors 包装后映射状态码；成功/失败均不在此处打访问日志。
func (s *Server) handleGuestLogin(writer http.ResponseWriter, request *http.Request) {
	body, err := decodeGuestLoginRequest(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, errs.CodeInvalidJSON, "请求体不是合法 JSON")
		return
	}

	result, err := s.auth.LoginGuestContext(request.Context(), body.Nickname)
	if err != nil {
		if errors.Is(err, errs.ErrInvalidNickname) {
			writeError(writer, http.StatusBadRequest, errs.CodeInvalidNickname, err.Error())
			return
		}
		// 非预期错误：仍只写响应；访问日志中间件会以 500 打 Error 级别一条。
		writeError(writer, http.StatusInternalServerError, errs.CodeInternal, "登录失败")
		return
	}

	writeJSON(writer, http.StatusOK, guestLoginResponse{
		PlayerID:  result.Player.ID,
		Nickname:  result.Player.Nickname,
		Token:     result.Token,
		ExpiresAt: result.ExpiresAt.Format(time.RFC3339),
	})
}

// decodeGuestLoginRequest 解析游客登录请求；允许空 body。
func decodeGuestLoginRequest(request *http.Request) (guestLoginRequest, error) {
	var body guestLoginRequest
	if request.Body == nil {
		return body, nil
	}
	defer request.Body.Close()

	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		if errors.Is(err, io.EOF) {
			return body, nil
		}
		return body, fmt.Errorf("decode guest login body: %w", err)
	}
	return body, nil
}

// errorResponse 为统一错误响应结构。
type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// writeJSON 统一写出 JSON 成功响应。
func writeJSON(writer http.ResponseWriter, statusCode int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(statusCode)
	_ = json.NewEncoder(writer).Encode(payload)
}

// writeError 统一写出 JSON 错误响应。
func writeError(writer http.ResponseWriter, statusCode int, errorCode, message string) {
	writeJSON(writer, statusCode, errorResponse{
		Error:   errorCode,
		Message: message,
	})
}
