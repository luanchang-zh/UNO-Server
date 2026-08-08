package room

import (
	"context"
	"fmt"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
)

// hasConnectedMember 判断房间内是否仍有至少一条有效玩家连接。
func (r *Room) hasConnectedMember() bool {
	for _, member := range r.Members {
		if member.Connected && member.Session != nil {
			return true
		}
	}
	return false
}

// scheduleEmptyRoomTimer 在房间首次变为空连接状态时启动一次性保留计时。
func (r *Room) scheduleEmptyRoomTimer() {
	if r.destroyed || r.emptyRoomTTL <= 0 || r.hasConnectedMember() || r.emptyTimer != nil {
		return
	}
	token := r.emptyToken
	r.emptyTimer = time.AfterFunc(r.emptyRoomTTL, func() {
		if err := r.submit(roomCommand{kind: commandEmptyRoomTimeout, emptyToken: token}); err != nil {
			r.logger.WithContext(context.Background()).Warn(
				"空房回收事件投递失败",
				"event", "room_gc_enqueue",
				"room_id", r.ID,
				"error", err,
			)
		}
	})
}

// cancelEmptyRoomTimer 停止空房计时并递增令牌，使已经入队的旧事件失效。
func (r *Room) cancelEmptyRoomTimer() {
	if r.emptyTimer != nil {
		r.emptyTimer.Stop()
		r.emptyTimer = nil
	}
	r.emptyToken++
}

// handleEmptyRoomTimeout 在 mailbox 内复核连接状态并将超时空房标记为销毁。
func (r *Room) handleEmptyRoomTimeout(token uint64) bool {
	if token != r.emptyToken || r.destroyed || r.hasConnectedMember() {
		return false
	}

	r.beginDestroy()

	if r.observer != nil {
		r.observer.ObserveRoomGarbageCollection(r.Phase)
	}
	r.logger.WithContext(context.Background()).Info(
		"空房超过保留时限，已回收",
		"event", "room_gc",
		"result", "collected",
		"room_id", r.ID,
		"phase", r.Phase,
		"member_count", len(r.Members),
		"empty_room_ttl_seconds", r.emptyRoomTTL.Seconds(),
	)
	if r.onDestroy != nil {
		r.onDestroy(r.ID)
	}
	return true
}

// beginDestroy 在 mailbox 内发布销毁状态，并停止所有可能继续投递事件的计时器。
func (r *Room) beginDestroy() {
	// 与 submit 使用同一把锁，保证关闭标记发布后不会再产生无人消费的同步命令。
	r.submitMu.Lock()
	r.closing = true
	r.submitMu.Unlock()
	r.destroyed = true
	r.cancelTurnTimer()
	r.cancelEmptyRoomTimer()
}

// rejectPendingCommands 在回收退出前唤醒已经排在回收事件之后的同步调用方。
func (r *Room) rejectPendingCommands() {
	roomClosedErr := fmt.Errorf("room garbage collected: %w", errs.ErrRoomNotFound)
	for {
		select {
		case pending := <-r.mailbox:
			if pending.done == nil {
				continue
			}
			if pending.kind == commandStop {
				pending.done <- nil
				continue
			}
			pending.done <- roomClosedErr
		default:
			return
		}
	}
}
