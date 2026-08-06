// Package room 负责房间生命周期、成员管理与房间内消息串行处理。
package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
	"github.com/luanchang-zh/UNO-Server/internal/session"
)

// 房间阶段。
const (
	// PhaseWaiting 等待准备。
	PhaseWaiting = "waiting"
	// PhasePlaying 对局中（M1 仅占位，未挂引擎）。
	PhasePlaying = "playing"
	// PhaseSettled 已结算。
	PhaseSettled = "settled"
)

const (
	// defaultMaxPlayers 默认人数上限。
	defaultMaxPlayers = 4
	// minPlayers 最少开局人数。
	minPlayers = 2
	// maxPlayersCap 人数上限最大值。
	maxPlayersCap = 6
	// mailboxSize 房间事件队列容量。
	mailboxSize = 64
)

// Member 房间内一名玩家。
type Member struct {
	PlayerID int64
	Nickname string
	Ready    bool
	Session  *session.Session
}

// Room 是对局容器：成员与阶段在 mailbox 协程内串行修改。
type Room struct {
	ID         string
	MaxPlayers int
	OwnerID    int64
	Phase      string
	Members    []*Member

	mailbox chan roomCommand
	closed  chan struct{}
	logger  *logx.Logger

	// onEmpty 房间空员时回调 Manager 销毁。
	onEmpty func(roomID string)
	// onMemberRemoved 任意成员离开时回调，用于清理 player→room 索引。
	onMemberRemoved func(roomID string, playerID int64)
	// once 保证 loop 只退出一次相关清理。
	stopOnce sync.Once
}

// roomCommand 进入 mailbox 的串行命令。
type roomCommand struct {
	ctx      context.Context
	session  *session.Session
	envelope protocol.Envelope
	kind     commandKind
	done     chan error // 可选，同步等待结果（测试用）
}

// commandKind 命令类型。
type commandKind int

const (
	commandMessage commandKind = iota
	commandJoin
	commandDisconnect
	commandStop
)

// roomHooks Manager 注入的生命周期回调。
type roomHooks struct {
	onEmpty         func(roomID string)
	onMemberRemoved func(roomID string, playerID int64)
}

// newRoom 创建房间并启动 mailbox 循环。
func newRoom(id string, maxPlayers int, owner *session.Session, logger *logx.Logger, hooks roomHooks) *Room {
	if maxPlayers < minPlayers || maxPlayers > maxPlayersCap {
		maxPlayers = defaultMaxPlayers
	}
	if logger == nil {
		logger = logx.NewFromSlog(nil)
	}

	room := &Room{
		ID:         id,
		MaxPlayers: maxPlayers,
		OwnerID:    owner.PlayerID,
		Phase:      PhaseWaiting,
		Members: []*Member{{
			PlayerID: owner.PlayerID,
			Nickname: owner.Nickname,
			Ready:    true, // 房主默认已准备
			Session:  owner,
		}},
		mailbox:         make(chan roomCommand, mailboxSize),
		closed:          make(chan struct{}),
		logger:          logger,
		onEmpty:         hooks.onEmpty,
		onMemberRemoved: hooks.onMemberRemoved,
	}
	go room.loop()
	return room
}

// loop 串行消费房间事件，是状态唯一写者。
func (r *Room) loop() {
	defer close(r.closed)
	for cmd := range r.mailbox {
		var err error
		switch cmd.kind {
		case commandStop:
			if cmd.done != nil {
				cmd.done <- nil
			}
			return
		case commandDisconnect:
			r.handleDisconnect(cmd.session)
		case commandJoin:
			err = r.tryJoin(cmd.session)
		case commandMessage:
			err = r.handleMessage(cmd.ctx, cmd.session, cmd.envelope)
		}
		if cmd.done != nil {
			cmd.done <- err
		}
	}
}

// submit 投递命令；房间已关闭则返回错误。
func (r *Room) submit(cmd roomCommand) error {
	select {
	case <-r.closed:
		return fmt.Errorf("room closed: %w", errs.ErrRoomNotFound)
	case r.mailbox <- cmd:
		return nil
	default:
		return fmt.Errorf("room mailbox full: %w", errs.ErrInternal)
	}
}

// submitSync 投递并等待处理完成（便于单测）。
func (r *Room) submitSync(ctx context.Context, kind commandKind, playerSession *session.Session, envelope protocol.Envelope) error {
	done := make(chan error, 1)
	if err := r.submit(roomCommand{
		ctx:      ctx,
		session:  playerSession,
		envelope: envelope,
		kind:     kind,
		done:     done,
	}); err != nil {
		return err
	}
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(3 * time.Second):
		return fmt.Errorf("room command timeout: %w", errs.ErrInternal)
	}
}

// handleMessage 处理房间内业务消息。
func (r *Room) handleMessage(ctx context.Context, playerSession *session.Session, envelope protocol.Envelope) error {
	_ = ctx
	switch envelope.Type {
	case protocol.TypeReady:
		return r.handleReady(playerSession, envelope)
	case protocol.TypeStart:
		return r.handleStart(playerSession, envelope)
	case protocol.TypeLeaveRoom:
		return r.handleLeave(playerSession, envelope)
	case protocol.TypeKick:
		return r.handleKick(playerSession, envelope)
	default:
		return fmt.Errorf("unsupported in room: %s: %w", envelope.Type, errs.ErrInvalidArgument)
	}
}

// handleReady 切换准备状态；房主始终视为已准备。
func (r *Room) handleReady(playerSession *session.Session, envelope protocol.Envelope) error {
	if r.Phase != PhaseWaiting {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeRoomPlaying, errs.ErrRoomAlreadyPlaying.Error())
	}
	member := r.findMember(playerSession.PlayerID)
	if member == nil {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeNotInRoom, errs.ErrNotInRoom.Error())
	}

	var payload protocol.ReadyPayload
	if len(envelope.Payload) > 0 {
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return r.replyError(playerSession, envelope.RequestID, errs.CodeInvalidJSON, "ready payload 非法")
		}
	}
	if member.PlayerID == r.OwnerID {
		member.Ready = true
	} else {
		member.Ready = payload.Ready
	}
	r.broadcastState()
	return nil
}

// handleStart 房主开局（M1 仅切换 phase，不发牌）。
func (r *Room) handleStart(playerSession *session.Session, envelope protocol.Envelope) error {
	if playerSession.PlayerID != r.OwnerID {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeNotRoomOwner, errs.ErrNotRoomOwner.Error())
	}
	if r.Phase != PhaseWaiting {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeRoomPlaying, errs.ErrRoomAlreadyPlaying.Error())
	}
	if len(r.Members) < minPlayers {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeRoomNotReady, "至少需要 2 名玩家")
	}
	for _, member := range r.Members {
		if !member.Ready {
			return r.replyError(playerSession, envelope.RequestID, errs.CodeRoomNotReady, errs.ErrRoomNotReady.Error())
		}
	}
	r.Phase = PhasePlaying
	r.broadcastState()
	return nil
}

// handleLeave 主动离开。
func (r *Room) handleLeave(playerSession *session.Session, envelope protocol.Envelope) error {
	_ = envelope
	r.removeMember(playerSession.PlayerID)
	return nil
}

// handleKick 房主踢人（仅 waiting）。
func (r *Room) handleKick(playerSession *session.Session, envelope protocol.Envelope) error {
	if playerSession.PlayerID != r.OwnerID {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeNotRoomOwner, errs.ErrNotRoomOwner.Error())
	}
	if r.Phase != PhaseWaiting {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeRoomPlaying, "对局中不可踢人")
	}
	var payload protocol.KickPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.PlayerID == 0 {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeInvalidArgument, "kick payload 非法")
	}
	if payload.PlayerID == r.OwnerID {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeInvalidArgument, "不能踢出房主")
	}
	target := r.findMember(payload.PlayerID)
	if target == nil {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeNotInRoom, "目标不在房间")
	}
	// 通知被踢者后移除。
	_ = r.replyError(target.Session, "", errs.CodeNotInRoom, "你已被房主移出房间")
	r.removeMember(payload.PlayerID)
	return nil
}

// handleDisconnect 连接断开：从房间移除。
func (r *Room) handleDisconnect(playerSession *session.Session) {
	if playerSession == nil {
		return
	}
	r.removeMember(playerSession.PlayerID)
}

// tryJoin 在房间协程内加入成员（由 Manager 经 submitSync 调用）。
func (r *Room) tryJoin(playerSession *session.Session) error {
	if r.Phase != PhaseWaiting {
		return errs.ErrRoomAlreadyPlaying
	}
	if r.findMember(playerSession.PlayerID) != nil {
		return errs.ErrAlreadyInRoom
	}
	if len(r.Members) >= r.MaxPlayers {
		return errs.ErrRoomFull
	}
	r.Members = append(r.Members, &Member{
		PlayerID: playerSession.PlayerID,
		Nickname: playerSession.Nickname,
		Ready:    false,
		Session:  playerSession,
	})
	r.broadcastState()
	return nil
}

// removeMember 移除成员并处理房主转移/空房。
func (r *Room) removeMember(playerID int64) {
	index := -1
	for i, member := range r.Members {
		if member.PlayerID == playerID {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	r.Members = append(r.Members[:index], r.Members[index+1:]...)
	if r.onMemberRemoved != nil {
		r.onMemberRemoved(r.ID, playerID)
	}

	if len(r.Members) == 0 {
		if r.onEmpty != nil {
			r.onEmpty(r.ID)
		}
		// 异步停止 loop，避免在 loop 内同步投递死锁。
		r.stopOnce.Do(func() {
			go func() {
				done := make(chan error, 1)
				_ = r.submit(roomCommand{kind: commandStop, done: done})
			}()
		})
		return
	}

	// 房主离开则转移给下一位。
	if playerID == r.OwnerID {
		r.OwnerID = r.Members[0].PlayerID
		r.Members[0].Ready = true
	}
	r.broadcastState()
}

// findMember 按 playerID 查找成员。
func (r *Room) findMember(playerID int64) *Member {
	for _, member := range r.Members {
		if member.PlayerID == playerID {
			return member
		}
	}
	return nil
}

// broadcastState 向房内所有连接推送 room_state。
func (r *Room) broadcastState() {
	payload := r.buildStatePayload()
	envelope, err := protocol.NewEnvelope(protocol.TypeRoomState, "", payload)
	if err != nil {
		return
	}
	for _, member := range r.Members {
		if member.Session == nil {
			continue
		}
		_ = member.Session.SendEnvelope(envelope)
	}
}

// buildStatePayload 构造公开房间视图。
func (r *Room) buildStatePayload() protocol.RoomStatePayload {
	members := make([]protocol.RoomMemberView, 0, len(r.Members))
	for _, member := range r.Members {
		members = append(members, protocol.RoomMemberView{
			PlayerID: member.PlayerID,
			Nickname: member.Nickname,
			Ready:    member.Ready,
			IsOwner:  member.PlayerID == r.OwnerID,
		})
	}
	return protocol.RoomStatePayload{
		RoomID:     r.ID,
		Phase:      r.Phase,
		MaxPlayers: r.MaxPlayers,
		OwnerID:    r.OwnerID,
		Members:    members,
	}
}

// replyError 向单个会话推送 error 消息。
func (r *Room) replyError(playerSession *session.Session, requestID, code, message string) error {
	if playerSession == nil {
		return errors.New(message)
	}
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
