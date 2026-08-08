package room

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
	"github.com/luanchang-zh/UNO-Server/internal/session"
)

const (
	defaultTurnTimeout        = 20 * time.Second
	defaultManagedActionDelay = 200 * time.Millisecond
	defaultTimeoutStrikeLimit = 2
)

// Options 控制房间回合计时和自动托管行为。
type Options struct {
	// TurnTimeout 是连接中玩家一次手动行动的等待上限。
	TurnTimeout time.Duration
	// ManagedActionDelay 是托管玩家两次自动行动之间的最小间隔。
	ManagedActionDelay time.Duration
	// TimeoutStrikeLimit 是连续超时多少次后进入托管。
	TimeoutStrikeLimit int
}

// Manager 管理全部房间，并作为 Session 的入站路由实现。
type Manager struct {
	mu         sync.RWMutex
	rooms      map[string]*Room
	playerRoom map[int64]string // 玩家 ID 到房间 ID 的索引
	logger     *logx.Logger
	options    Options
}

// NewManager 创建房间管理器。
func NewManager(logger *logx.Logger, options Options) *Manager {
	if logger == nil {
		logger = logx.NewFromSlog(nil)
	}
	options = normalizeOptions(options)
	return &Manager{
		rooms:      make(map[string]*Room),
		playerRoom: make(map[int64]string),
		logger:     logger,
		options:    options,
	}
}

// normalizeOptions 为未设置的房间运行参数填充安全默认值。
func normalizeOptions(options Options) Options {
	if options.TurnTimeout <= 0 {
		options.TurnTimeout = defaultTurnTimeout
	}
	if options.ManagedActionDelay <= 0 {
		options.ManagedActionDelay = defaultManagedActionDelay
	}
	if options.TimeoutStrikeLimit <= 0 {
		options.TimeoutStrikeLimit = defaultTimeoutStrikeLimit
	}
	return options
}

// Route 实现 session.InboundRouter：处理房间类消息。
func (m *Manager) Route(ctx context.Context, playerSession *session.Session, envelope protocol.Envelope) error {
	switch envelope.Type {
	case protocol.TypeCreateRoom:
		return m.handleCreateRoom(ctx, playerSession, envelope)
	case protocol.TypeJoinRoom:
		return m.handleJoinRoom(ctx, playerSession, envelope)
	case protocol.TypeLeaveRoom, protocol.TypeReady, protocol.TypeStart, protocol.TypeKick,
		protocol.TypePlayCard, protocol.TypeDrawCard, protocol.TypePass,
		protocol.TypeChooseColor, protocol.TypeCallUNO, protocol.TypeCatchUNO:
		return m.forwardToPlayerRoom(ctx, playerSession, envelope)
	default:
		return fmt.Errorf("unknown type %q: %w", envelope.Type, errs.ErrInvalidArgument)
	}
}

// OnSessionOpen 在新连接鉴权后尝试换绑玩家仍保留的房间座位。
func (m *Manager) OnSessionOpen(playerSession *session.Session) error {
	if playerSession == nil {
		return nil
	}
	m.mu.RLock()
	roomID, found := m.playerRoom[playerSession.PlayerID]
	target := m.rooms[roomID]
	m.mu.RUnlock()
	if !found {
		return nil
	}
	if target == nil {
		m.clearPlayerRoom(roomID, playerSession.PlayerID)
		return nil
	}
	return target.submitSync(context.Background(), commandReconnect, playerSession, protocol.Envelope{})
}

// OnSessionClose 将断线事件交给房间；是否保留座位由房间阶段决定。
func (m *Manager) OnSessionClose(playerSession *session.Session) {
	if playerSession == nil {
		return
	}
	m.mu.RLock()
	roomID, ok := m.playerRoom[playerSession.PlayerID]
	var target *Room
	if ok {
		target = m.rooms[roomID]
	}
	m.mu.RUnlock()
	if target == nil {
		if ok {
			m.clearPlayerRoom(roomID, playerSession.PlayerID)
		}
		return
	}
	// 同步处理，避免断线后的座位状态与索引短暂不一致。
	_ = target.submitSync(context.Background(), commandDisconnect, playerSession, protocol.Envelope{})
}

// handleCreateRoom 创建房间并将创建者设为房主。
func (m *Manager) handleCreateRoom(ctx context.Context, playerSession *session.Session, envelope protocol.Envelope) error {
	if m.playerRoomID(playerSession.PlayerID) != "" {
		return m.sendError(playerSession, envelope.RequestID, errs.CodeAlreadyInRoom, errs.ErrAlreadyInRoom.Error())
	}

	maxPlayers := defaultMaxPlayers
	if len(envelope.Payload) > 0 {
		var payload protocol.CreateRoomPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return m.sendError(playerSession, envelope.RequestID, errs.CodeInvalidJSON, "create_room payload 非法")
		}
		if payload.MaxPlayers != 0 {
			if payload.MaxPlayers < minPlayers || payload.MaxPlayers > maxPlayersCap {
				return m.sendError(playerSession, envelope.RequestID, errs.CodeInvalidArgument, "max_players 须在 2–6")
			}
			maxPlayers = payload.MaxPlayers
		}
	}

	roomID, err := m.allocateRoomID()
	if err != nil {
		return m.sendError(playerSession, envelope.RequestID, errs.CodeInternal, "分配房间号失败")
	}

	created := newRoom(roomID, maxPlayers, playerSession, m.logger, m.options, roomHooks{
		onEmpty:         m.removeRoom,
		onMemberRemoved: m.clearPlayerRoom,
	})
	m.mu.Lock()
	m.rooms[roomID] = created
	m.playerRoom[playerSession.PlayerID] = roomID
	m.mu.Unlock()

	// 初始状态仅发给房主；后续变更一律在房间 mailbox 内广播，避免跨协程读 Members。
	_ = playerSession.SendEnvelope(mustRoomStateEnvelope(created.ID, created.MaxPlayers, created.OwnerID, playerSession))
	_ = ctx
	return nil
}

// mustRoomStateEnvelope 构造仅含房主一人的初始 room_state。
func mustRoomStateEnvelope(roomID string, maxPlayers int, ownerID int64, owner *session.Session) protocol.Envelope {
	envelope, err := protocol.NewEnvelope(protocol.TypeRoomState, "", protocol.RoomStatePayload{
		RoomID:     roomID,
		Phase:      PhaseWaiting,
		MaxPlayers: maxPlayers,
		OwnerID:    ownerID,
		Members: []protocol.RoomMemberView{{
			PlayerID:  owner.PlayerID,
			Nickname:  owner.Nickname,
			Ready:     true,
			IsOwner:   true,
			Connected: true,
		}},
	})
	if err != nil {
		return protocol.Envelope{Type: protocol.TypeRoomState}
	}
	return envelope
}

// handleJoinRoom 加入已有房间。
func (m *Manager) handleJoinRoom(ctx context.Context, playerSession *session.Session, envelope protocol.Envelope) error {
	if m.playerRoomID(playerSession.PlayerID) != "" {
		return m.sendError(playerSession, envelope.RequestID, errs.CodeAlreadyInRoom, errs.ErrAlreadyInRoom.Error())
	}

	var payload protocol.JoinRoomPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || strings.TrimSpace(payload.RoomID) == "" {
		return m.sendError(playerSession, envelope.RequestID, errs.CodeInvalidArgument, "room_id 必填")
	}
	roomID := strings.ToUpper(strings.TrimSpace(payload.RoomID))

	m.mu.RLock()
	target := m.rooms[roomID]
	m.mu.RUnlock()
	if target == nil {
		return m.sendError(playerSession, envelope.RequestID, errs.CodeRoomNotFound, errs.ErrRoomNotFound.Error())
	}

	err := target.submitSync(ctx, commandJoin, playerSession, envelope)
	if err != nil {
		code, message := mapRoomError(err)
		return m.sendError(playerSession, envelope.RequestID, code, message)
	}

	m.mu.Lock()
	m.playerRoom[playerSession.PlayerID] = roomID
	m.mu.Unlock()
	return nil
}

// forwardToPlayerRoom 将消息转发到玩家所在房间 mailbox。
func (m *Manager) forwardToPlayerRoom(ctx context.Context, playerSession *session.Session, envelope protocol.Envelope) error {
	roomID := m.playerRoomID(playerSession.PlayerID)
	if roomID == "" {
		return m.sendError(playerSession, envelope.RequestID, errs.CodeNotInRoom, errs.ErrNotInRoom.Error())
	}
	m.mu.RLock()
	target := m.rooms[roomID]
	m.mu.RUnlock()
	if target == nil {
		return m.sendError(playerSession, envelope.RequestID, errs.CodeRoomNotFound, errs.ErrRoomNotFound.Error())
	}

	// leave 成功后需清理 playerRoom 索引。
	err := target.submitSync(ctx, commandMessage, playerSession, envelope)
	if envelope.Type == protocol.TypeLeaveRoom {
		m.mu.Lock()
		// 仅当仍指向该房时删除（避免误删重进后的映射）。
		if m.playerRoom[playerSession.PlayerID] == roomID {
			delete(m.playerRoom, playerSession.PlayerID)
		}
		m.mu.Unlock()
	}
	if err != nil {
		// 业务错误已在房间内推送 error 时，此处仍返回 err 供日志 result 使用。
		return err
	}
	return nil
}

// removeRoom 空房回调：从索引移除并清理成员映射。
func (m *Manager) removeRoom(roomID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rooms, roomID)
	for playerID, id := range m.playerRoom {
		if id == roomID {
			delete(m.playerRoom, playerID)
		}
	}
}

// clearPlayerRoom 成员离开（leave/kick/disconnect）时清理 player→room 映射。
func (m *Manager) clearPlayerRoom(roomID string, playerID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.playerRoom[playerID] == roomID {
		delete(m.playerRoom, playerID)
	}
}

// playerRoomID 查询玩家所在房间。
func (m *Manager) playerRoomID(playerID int64) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.playerRoom[playerID]
}

// allocateRoomID 生成不冲突的 6 位房间号。
func (m *Manager) allocateRoomID() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	const length = 6
	for attempt := 0; attempt < 32; attempt++ {
		var builder strings.Builder
		builder.Grow(length)
		for i := 0; i < length; i++ {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				return "", err
			}
			builder.WriteByte(alphabet[n.Int64()])
		}
		candidate := builder.String()
		m.mu.RLock()
		_, exists := m.rooms[candidate]
		m.mu.RUnlock()
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("room id exhausted: %w", errs.ErrInternal)
}

// sendError 向会话推送 error。
func (m *Manager) sendError(playerSession *session.Session, requestID, code, message string) error {
	envelope, err := protocol.NewEnvelope(protocol.TypeError, requestID, protocol.ErrorPayload{
		Code:    code,
		Message: message,
	})
	if err != nil {
		return err
	}
	_ = playerSession.SendEnvelope(envelope)
	return fmt.Errorf("%s", message)
}

// mapRoomError 将房间错误映射为对外 code。
func mapRoomError(err error) (code, message string) {
	switch {
	case errors.Is(err, errs.ErrRoomFull):
		return errs.CodeRoomFull, errs.ErrRoomFull.Error()
	case errors.Is(err, errs.ErrRoomAlreadyPlaying):
		return errs.CodeRoomPlaying, errs.ErrRoomAlreadyPlaying.Error()
	case errors.Is(err, errs.ErrAlreadyInRoom):
		return errs.CodeAlreadyInRoom, errs.ErrAlreadyInRoom.Error()
	case errors.Is(err, errs.ErrRoomNotFound):
		return errs.CodeRoomNotFound, errs.ErrRoomNotFound.Error()
	default:
		return errs.CodeInternal, err.Error()
	}
}

// Count 返回当前房间数（测试/观测）。
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rooms)
}
