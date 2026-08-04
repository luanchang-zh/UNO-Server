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
// 本 handler 会阻塞直到连接关闭，以便外层访问日志记录整段 WS 时长。
func (s *Server) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	token := request.URL.Query().Get("token")
	authSession, err := s.auth.Authenticate(token)
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
		ID:       logx.NewTraceID(),
		PlayerID: authSession.PlayerID,
		Nickname: authSession.Nickname,
		Conn:     conn,
		Logger:   s.logger,
	})
	if err != nil {
		_ = conn.Close()
		return
	}
	defer playerSession.Close()

	if err := playerSession.SendHello(); err != nil {
		return
	}

	// 阻塞至会话结束或请求上下文取消（进程关闭时）。
	select {
	case <-playerSession.Done():
	case <-request.Context().Done():
	}
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
