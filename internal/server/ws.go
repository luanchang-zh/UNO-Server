package server

import (
	"errors"
	"net/http"

	"github.com/gorilla/websocket"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/session"
)

// upgrader 将 HTTP 连接升级为 WebSocket；CheckOrigin 开发期放行，生产应收紧。
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(_ *http.Request) bool {
		// 练手阶段允许任意 Origin；上线前改为白名单校验。
		return true
	},
}

// handleWebSocket 校验 token 后升级连接，启动 Session 并下发 hello。
//
// 升级成功后立即返回：每连接仅保留 read/write 两个泵；
// 握手访问日志由中间件在本 handler 返回时打出；
// 断线日志与登记清理由 Session.Close / Manager 负责。
func (s *Server) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	token := request.URL.Query().Get("token")
	authSession, err := s.auth.AuthenticateContext(request.Context(), token)
	if err != nil {
		status, code, message := mapAuthError(err)
		writeError(writer, status, code, message)
		return
	}

	conn, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		// Upgrade 内部可能已写响应；此处仅补日志。
		s.logger.WithContext(request.Context()).Warn("websocket upgrade failed", "error", err)
		return
	}

	playerSession, err := session.New(session.Options{
		ID:        logx.NewTraceID(),
		PlayerID:  authSession.PlayerID,
		Nickname:  authSession.Nickname,
		Conn:      conn,
		Logger:    s.logger,
		Manager:   s.sessions,
		Router:    s.rooms,
		CloseHook: s.rooms,
	})
	if err != nil {
		_ = conn.Close()
		return
	}

	if err := playerSession.SendHello(); err != nil {
		// 发送失败则主动关闭（会 Unregister + 断线日志）。
		playerSession.Close()
		return
	}
	if err := s.rooms.OnSessionOpen(playerSession); err != nil {
		// 重连换绑失败不关闭基础连接，玩家仍可收到明确错误后重新选择操作。
		s.logger.WithContext(request.Context()).Warn(
			"websocket room rebind failed",
			"player_id", playerSession.PlayerID,
			"error", err,
		)
	}
	// 不再阻塞等待 Done；连接由泵与 Shutdown→CloseAll 管理。
}

// mapAuthError 将鉴权错误映射为 HTTP 状态码与对外错误码。
func mapAuthError(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, errs.ErrTokenExpired):
		return http.StatusUnauthorized, errs.CodeTokenExpired, "token 已过期"
	case errors.Is(err, errs.ErrTokenNotFound):
		return http.StatusUnauthorized, errs.CodeTokenNotFound, "token 无效"
	default:
		return http.StatusUnauthorized, errs.CodeUnauthorized, "未授权"
	}
}
