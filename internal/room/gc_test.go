package room

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/session"
)

// TestRecoveredEmptyRoomIsGarbageCollected 验证重启后无人重连的等待房会按时清理内存和 Redis 快照。
func TestRecoveredEmptyRoomIsGarbageCollected(t *testing.T) {
	repository := newMemorySnapshotRepository()
	repository.put(t, roomSnapshot{
		SchemaVersion: roomSnapshotSchemaVersion,
		Revision:      1,
		UpdatedAt:     time.Now().UTC(),
		RoomID:        "IDLE01",
		MaxPlayers:    4,
		OwnerID:       101,
		Phase:         PhaseWaiting,
		Members:       []memberSnapshot{{PlayerID: 101, Nickname: "等待玩家", Ready: true}},
	})
	observer := &recordingRoomObserver{}
	manager := NewManager(nil, Options{
		SnapshotRepository: repository,
		SnapshotTimeout:    time.Second,
		EmptyRoomTTL:       25 * time.Millisecond,
		Observer:           observer,
	})
	if err := manager.Restore(context.Background()); err != nil {
		t.Fatalf("恢复等待房失败：%v", err)
	}
	if manager.Count() != 1 {
		t.Fatalf("恢复后房间数=%d", manager.Count())
	}

	waitUntil(t, time.Second, func() bool {
		repository.mu.Lock()
		_, snapshotExists := repository.records["IDLE01"]
		repository.mu.Unlock()
		return manager.Count() == 0 && !snapshotExists
	})
	if manager.playerRoomID(101) != "" {
		t.Fatal("空房回收后玩家路由仍然存在")
	}
	if !observer.hasRestoreResult("restored") || !observer.hasGarbageCollection(PhaseWaiting) {
		t.Fatalf("观测事件不完整：restore=%v gc=%v", observer.restoreResults, observer.gcPhases)
	}
}

// TestEmptyRoomTimerRejectsStaleEvent 验证成员重新连接后，旧回收事件不能销毁房间。
func TestEmptyRoomTimerRejectsStaleEvent(t *testing.T) {
	target := &Room{
		ID:           "KEEP01",
		Phase:        PhaseWaiting,
		Members:      []*Member{{PlayerID: 7, Nickname: "玩家", Connected: false}},
		emptyRoomTTL: time.Hour,
		mailbox:      make(chan roomCommand, mailboxSize),
		closed:       make(chan struct{}),
		logger:       NewManager(nil, Options{}).logger,
	}
	target.scheduleEmptyRoomTimer()
	staleToken := target.emptyToken
	target.Members[0].Connected = true
	target.Members[0].Session = &session.Session{PlayerID: 7}
	target.cancelEmptyRoomTimer()
	if target.handleEmptyRoomTimeout(staleToken) {
		t.Fatal("重连后的房间被旧回收事件销毁")
	}
	if target.destroyed || target.closing {
		t.Fatal("拒绝旧事件后房间进入了关闭状态")
	}
}

// TestRejectPendingCommands 验证回收退出会区分停机命令并唤醒其他同步调用方。
func TestRejectPendingCommands(t *testing.T) {
	target := &Room{mailbox: make(chan roomCommand, 2)}
	stopDone := make(chan error, 1)
	messageDone := make(chan error, 1)
	target.mailbox <- roomCommand{kind: commandStop, done: stopDone}
	target.mailbox <- roomCommand{kind: commandMessage, done: messageDone}

	target.rejectPendingCommands()
	if err := <-stopDone; err != nil {
		t.Fatalf("并发停机命令收到错误：%v", err)
	}
	if err := <-messageDone; !errors.Is(err, errs.ErrRoomNotFound) {
		t.Fatalf("普通同步命令错误=%v", err)
	}
}

// TestShutdownIgnoresConcurrentGarbageCollection 验证停机与空房回收相遇时不会报告伪失败。
func TestShutdownIgnoresConcurrentGarbageCollection(t *testing.T) {
	manager := NewManager(nil, Options{})
	target := &Room{
		ID:      "CLOSE1",
		mailbox: make(chan roomCommand, 1),
		closed:  make(chan struct{}),
		closing: true,
	}
	manager.rooms[target.ID] = target
	if err := manager.Shutdown(context.Background()); err != nil {
		t.Fatalf("并发回收导致停机失败：%v", err)
	}
}

// TestRemoveLastMemberBeginsDestroy 验证最后一名成员离开会在当前 mailbox 命令内发布关闭状态。
func TestRemoveLastMemberBeginsDestroy(t *testing.T) {
	destroyedRoomID := ""
	target := &Room{
		ID:      "EMPTY1",
		Phase:   PhaseWaiting,
		Members: []*Member{{PlayerID: 9, Nickname: "最后玩家"}},
		mailbox: make(chan roomCommand, 1),
		closed:  make(chan struct{}),
		onDestroy: func(roomID string) {
			destroyedRoomID = roomID
		},
	}
	target.removeMember(9)
	if !target.destroyed || !target.closing || destroyedRoomID != target.ID {
		t.Fatalf(
			"最后成员离开后的状态错误：destroyed=%v closing=%v callback=%q",
			target.destroyed,
			target.closing,
			destroyedRoomID,
		)
	}
}

// waitUntil 在限定时间内轮询异步状态，避免依赖固定长时间休眠。
func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待异步条件超时")
}

// recordingRoomObserver 线程安全记录房间观测事件，供计时器测试断言。
type recordingRoomObserver struct {
	mu             sync.Mutex
	gcPhases       []string
	restoreResults []string
}

// ObserveRoomGarbageCollection 记录空房回收阶段。
func (o *recordingRoomObserver) ObserveRoomGarbageCollection(phase string) {
	o.mu.Lock()
	o.gcPhases = append(o.gcPhases, phase)
	o.mu.Unlock()
}

// ObserveRoomRestore 记录单条快照恢复结果。
func (o *recordingRoomObserver) ObserveRoomRestore(result string) {
	o.mu.Lock()
	o.restoreResults = append(o.restoreResults, result)
	o.mu.Unlock()
}

// hasGarbageCollection 查询是否记录了指定阶段的回收事件。
func (o *recordingRoomObserver) hasGarbageCollection(phase string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, recorded := range o.gcPhases {
		if recorded == phase {
			return true
		}
	}
	return false
}

// hasRestoreResult 查询是否记录了指定恢复结果。
func (o *recordingRoomObserver) hasRestoreResult(result string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, recorded := range o.restoreResults {
		if recorded == result {
			return true
		}
	}
	return false
}
