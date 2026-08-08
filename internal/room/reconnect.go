package room

import (
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/session"
)

// handleDisconnect 等待阶段直接移除成员，对局中则保留座位并立即进入托管。
func (r *Room) handleDisconnect(playerSession *session.Session) {
	if playerSession == nil {
		return
	}
	member := r.findMember(playerSession.PlayerID)
	// 旧连接在重连换绑后才关闭时，不得覆盖新连接状态。
	if member == nil || member.Session != playerSession {
		return
	}
	if r.Phase == PhasePlaying {
		member.Session = nil
		member.Connected = false
		member.AutoPlay = true
		if !r.hasConnectedMember() {
			r.scheduleEmptyRoomTimer()
		}
		currentActor := r.isCurrentGamePlayer(member.PlayerID)
		if currentActor {
			r.scheduleTurnTimer()
		}
		r.broadcastState()
		if currentActor {
			// 当前行动时限已切换为托管延迟，需要同步刷新其他在线玩家的截止时间。
			r.broadcastGameState()
		}
		return
	}
	r.removeMember(playerSession.PlayerID)
}

// tryReconnect 将新会话换绑到保留座位，并向该连接发送房间与牌局全量状态。
func (r *Room) tryReconnect(playerSession *session.Session) error {
	if playerSession == nil {
		return errs.ErrNotInRoom
	}
	member := r.findMember(playerSession.PlayerID)
	if member == nil || member.abandoned {
		return errs.ErrNotInRoom
	}

	previousSession := member.Session
	member.Session = playerSession
	member.Connected = true
	member.AutoPlay = false
	member.TimeoutStrikes = 0
	r.cancelEmptyRoomTimer()
	currentActor := r.Phase == PhasePlaying && r.isCurrentGamePlayer(member.PlayerID)
	if currentActor {
		r.scheduleTurnTimer()
	}
	r.broadcastState()
	if currentActor {
		// 换绑当前行动者会重置完整时限，因此所有在线玩家都要收到相同的新截止时间。
		r.broadcastGameState()
	} else {
		r.sendGameStateTo(member)
	}

	// 先完成房间内换绑，再异步关闭旧连接；其迟到的断线事件会因指针不匹配而被忽略。
	if previousSession != nil && previousSession != playerSession {
		go previousSession.Close()
	}
	return nil
}

// abandonMember 让对局中主动离开的玩家脱离房间路由，但保留引擎座位托管到终局。
func (r *Room) abandonMember(playerID int64) {
	member := r.findMember(playerID)
	if member == nil {
		return
	}
	member.Session = nil
	member.Connected = false
	member.AutoPlay = true
	member.abandoned = true
	if r.onMemberRemoved != nil {
		r.onMemberRemoved(r.ID, playerID)
	}
	currentActor := r.isCurrentGamePlayer(playerID)
	if currentActor {
		r.scheduleTurnTimer()
	}
	if !r.hasConnectedMember() {
		r.scheduleEmptyRoomTimer()
	}
	r.broadcastState()
	if currentActor {
		r.broadcastGameState()
	}
}
