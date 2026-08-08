// Package session 管理 WebSocket 连接生命周期：读写泵、出站队列与基础消息处理。
package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
)

const (
	// sendBufferSize 出站队列容量；满则断开，避免拖死写路径。
	sendBufferSize = 64
	// writeWait 单次写超时。
	writeWait = 10 * time.Second
	// readIdleWait 两次入站消息之间的最长空闲时间。
	readIdleWait = 60 * time.Second
	// maxMessageSize 限制单帧大小，防止恶意大包。
	maxMessageSize = 4 << 10
)

// InboundRouter 将非内建消息（非 ping）路由到业务层（如房间）。
type InboundRouter interface {
	Route(ctx context.Context, playerSession *Session, envelope protocol.Envelope) error
}

// CloseHook 会话关闭时的回调（如离开房间）。
type CloseHook interface {
	OnSessionClose(playerSession *Session)
}

// Session 表示一条已鉴权的 WebSocket 连接。
type Session struct {
	// ID 连接级唯一标识。
	ID string
	// PlayerID 玩家业务身份。
	PlayerID int64
	// Nickname 展示昵称。
	Nickname string

	conn        *websocket.Conn
	send        chan []byte
	logger      *logx.Logger
	manager     *Manager
	router      InboundRouter
	closeHook   CloseHook
	remoteAddr  string
	connectedAt time.Time

	closeOnce sync.Once
	closed    chan struct{}
}

// Options 创建 Session 所需依赖。
type Options struct {
	// ID 连接 ID。
	ID string
	// PlayerID 玩家 ID。
	PlayerID int64
	// Nickname 昵称。
	Nickname string
	// Conn 已升级的 WebSocket 连接。
	Conn *websocket.Conn
	// Logger 日志器。
	Logger *logx.Logger
	// Manager 可选，非空时自动 Register，Close 时 Unregister。
	Manager *Manager
	// Router 可选，处理房间等业务消息。
	Router InboundRouter
	// CloseHook 可选，断线时通知业务层。
	CloseHook CloseHook
}

// New 创建会话、登记 Manager（若有）并启动读写泵。
// 调用方不应在 handler 里阻塞等待；连接由泵与 Manager.CloseAll 管理。
func New(options Options) (*Session, error) {
	if options.Conn == nil {
		return nil, fmt.Errorf("session: conn is nil: %w", errs.ErrInvalidArgument)
	}
	if options.Logger == nil {
		options.Logger = logx.NewFromSlog(nil)
	}
	if options.ID == "" {
		options.ID = logx.NewTraceID()
	}

	remoteAddr := ""
	if options.Conn.RemoteAddr() != nil {
		remoteAddr = options.Conn.RemoteAddr().String()
	}

	playerSession := &Session{
		ID:          options.ID,
		PlayerID:    options.PlayerID,
		Nickname:    options.Nickname,
		conn:        options.Conn,
		send:        make(chan []byte, sendBufferSize),
		logger:      options.Logger,
		manager:     options.Manager,
		router:      options.Router,
		closeHook:   options.CloseHook,
		remoteAddr:  remoteAddr,
		connectedAt: time.Now(),
		closed:      make(chan struct{}),
	}

	if options.Manager != nil {
		options.Manager.Register(playerSession)
	}

	go playerSession.writePump()
	go playerSession.readPump()

	return playerSession, nil
}

// Done 在会话关闭时关闭，供可选等待。
func (s *Session) Done() <-chan struct{} {
	return s.closed
}

// SendEnvelope 将协议消息放入出站队列。
func (s *Session) SendEnvelope(envelope protocol.Envelope) error {
	data, err := protocol.Encode(envelope)
	if err != nil {
		return err
	}
	return s.Send(data)
}

// Send 发送原始 JSON 字节；队列满则关闭连接。
func (s *Session) Send(data []byte) error {
	select {
	case <-s.closed:
		return fmt.Errorf("session closed: %w", errs.ErrInternal)
	case s.send <- data:
		return nil
	default:
		// 背压：异步断开慢客户端，避免业务 actor 在关闭回调中同步回投自身而死锁。
		go s.Close()
		return fmt.Errorf("send buffer full: %w", errs.ErrInternal)
	}
}

// Close 关闭连接、注销 Manager 并打断线日志；可重复调用。
func (s *Session) Close() {
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.conn != nil {
			_ = s.conn.Close()
		}

		if s.manager != nil {
			s.manager.Unregister(s.ID)
		}
		if s.closeHook != nil {
			s.closeHook.OnSessionClose(s)
		}

		// 断线日志：覆盖整段在线时长（与握手访问日志成对）。
		ctx := logx.IntoContext(context.Background(), s.logger, logx.NewTraceID())
		s.logger.WithContext(ctx).Info("ws connection closed",
			"conn_id", s.ID,
			"player_id", s.PlayerID,
			"remote", s.remoteAddr,
			"duration_ms", time.Since(s.connectedAt).Milliseconds(),
		)
	})
}

// SendHello 在连接建立后推送身份确认。
func (s *Session) SendHello() error {
	envelope, err := protocol.NewEnvelope(protocol.TypeHello, "", protocol.HelloPayload{
		PlayerID: s.PlayerID,
		Nickname: s.Nickname,
	})
	if err != nil {
		return err
	}
	return s.SendEnvelope(envelope)
}

// readPump 持续读取客户端消息；每个入站包处理结束打一条日志。
func (s *Session) readPump() {
	defer s.Close()

	s.conn.SetReadLimit(maxMessageSize)
	_ = s.conn.SetReadDeadline(time.Now().Add(readIdleWait))

	for {
		messageType, payload, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		// 有活动则刷新读超时，避免长连接被 idle 掐断。
		_ = s.conn.SetReadDeadline(time.Now().Add(readIdleWait))

		if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
			continue
		}

		// 每个 WS 包独立 trace_id，与「一包一条日志」约定一致。
		ctx := logx.IntoContext(context.Background(), s.logger, logx.NewTraceID())
		s.handleInbound(ctx, payload)
	}
}

// handleInbound 解析并处理单条入站消息，结束时打一条日志。
func (s *Session) handleInbound(ctx context.Context, payload []byte) {
	startedAt := time.Now()
	messageType := "unknown"
	result := "ok"
	var handleErr error

	defer func() {
		fields := []any{
			"conn_id", s.ID,
			"player_id", s.PlayerID,
			"type", messageType,
			"result", result,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		}
		contextLogger := s.logger.WithContext(ctx)
		if handleErr != nil {
			fields = append(fields, "error", handleErr.Error())
			contextLogger.Warn("ws message", fields...)
			return
		}
		contextLogger.Info("ws message", fields...)
	}()

	envelope, err := protocol.Decode(payload)
	if err != nil {
		handleErr = err
		result = "bad_envelope"
		_ = s.sendError("", errs.CodeInvalidJSON, "消息不是合法 JSON 信封")
		return
	}
	messageType = envelope.Type

	switch envelope.Type {
	case protocol.TypePing:
		handleErr = s.handlePing(envelope)
		if handleErr != nil {
			result = "error"
		}
	default:
		if s.router == nil {
			handleErr = fmt.Errorf("unknown type %q: %w", envelope.Type, errs.ErrInvalidArgument)
			result = "unknown_type"
			_ = s.sendError(envelope.RequestID, errs.CodeInvalidArgument, "未知消息类型: "+envelope.Type)
			return
		}
		handleErr = s.router.Route(ctx, s, envelope)
		if handleErr != nil {
			result = "error"
		}
	}
}

// handlePing 回复 pong，回显 request_id 与 payload。
func (s *Session) handlePing(envelope protocol.Envelope) error {
	response := protocol.Envelope{
		Type:      protocol.TypePong,
		RequestID: envelope.RequestID,
		Payload:   envelope.Payload,
	}
	return s.SendEnvelope(response)
}

// sendError 向客户端推送 error 消息。
func (s *Session) sendError(requestID, code, message string) error {
	envelope, err := protocol.NewEnvelope(protocol.TypeError, requestID, protocol.ErrorPayload{
		Code:    code,
		Message: message,
	})
	if err != nil {
		return err
	}
	return s.SendEnvelope(envelope)
}

// writePump 从 send 通道取消息写入连接，直到会话关闭。
func (s *Session) writePump() {
	defer func() {
		_ = s.conn.Close()
	}()

	for {
		select {
		case <-s.closed:
			return
		case message := <-s.send:
			_ = s.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := s.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				// 写失败时主动 Close，触发断线日志与 Unregister。
				s.Close()
				return
			}
		}
	}
}
