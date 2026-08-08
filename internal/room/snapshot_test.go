package room

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// TestManagerRestorePlayingRoom 验证重启后完整恢复引擎、MySQL 关联和玩家路由。
func TestManagerRestorePlayingRoom(t *testing.T) {
	repository := newMemorySnapshotRepository()
	engine, err := uno.New([]int64{101, 202}, uno.Config{})
	if err != nil {
		t.Fatalf("创建引擎失败：%v", err)
	}
	wantEngine := engine.Snapshot()
	deadline := time.Now().UTC().Add(time.Hour)
	startedAt := time.Now().UTC().Add(-time.Minute)
	snapshot := roomSnapshot{
		SchemaVersion: roomSnapshotSchemaVersion,
		Revision:      7,
		UpdatedAt:     time.Now().UTC(),
		RoomID:        "REST01",
		MaxPlayers:    4,
		OwnerID:       101,
		Phase:         PhasePlaying,
		Members: []memberSnapshot{
			{PlayerID: 101, Nickname: "房主", Ready: true, Connected: true},
			{PlayerID: 202, Nickname: "玩家乙", Ready: true, Connected: true, TimeoutStrikes: 1},
		},
		Engine:         &wantEngine,
		TurnDeadline:   &deadline,
		MatchID:        9001,
		MatchStartedAt: &startedAt,
	}
	repository.put(t, snapshot)
	manager := NewManager(nil, Options{
		TurnTimeout:        time.Hour,
		ManagedActionDelay: time.Hour,
		SnapshotRepository: repository,
		SnapshotTimeout:    time.Second,
		SnapshotTTL:        2 * time.Hour,
	})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })

	if err := manager.Restore(context.Background()); err != nil {
		t.Fatalf("恢复房间失败：%v", err)
	}
	if manager.Count() != 1 || manager.playerRoomID(101) != "REST01" || manager.playerRoomID(202) != "REST01" {
		t.Fatalf("恢复后的索引不正确：count=%d owner=%q guest=%q", manager.Count(), manager.playerRoomID(101), manager.playerRoomID(202))
	}
	manager.mu.RLock()
	restored := manager.rooms["REST01"]
	manager.mu.RUnlock()
	if restored == nil || restored.matchID != 9001 || !restored.matchStartedAt.Equal(startedAt) {
		t.Fatalf("恢复后的对局元数据不正确：%+v", restored)
	}
	for _, member := range restored.Members {
		if member.Connected || member.Session != nil || !member.AutoPlay {
			t.Fatalf("恢复成员未统一进入断线托管：%+v", member)
		}
	}
	if got := restored.engine.Snapshot(); !reflect.DeepEqual(got, wantEngine) {
		t.Fatal("恢复后的 UNO 权威状态发生变化")
	}
	if repository.saveCount != 1 {
		t.Fatalf("恢复后快照刷新次数=%d", repository.saveCount)
	}
}

// TestManagerRestoreWaitingRoom 验证等待房间恢复后保留座位但不会错误进入托管。
func TestManagerRestoreWaitingRoom(t *testing.T) {
	repository := newMemorySnapshotRepository()
	repository.put(t, roomSnapshot{
		SchemaVersion: roomSnapshotSchemaVersion,
		Revision:      2,
		UpdatedAt:     time.Now().UTC(),
		RoomID:        "WAIT01",
		MaxPlayers:    4,
		OwnerID:       303,
		Phase:         PhaseWaiting,
		Members:       []memberSnapshot{{PlayerID: 303, Nickname: "等待玩家", Ready: true, Connected: true}},
	})
	manager := NewManager(nil, Options{SnapshotRepository: repository})
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	if err := manager.Restore(context.Background()); err != nil {
		t.Fatalf("恢复等待房间失败：%v", err)
	}
	manager.mu.RLock()
	restored := manager.rooms["WAIT01"]
	manager.mu.RUnlock()
	if restored == nil || restored.engine != nil || restored.Members[0].Connected || restored.Members[0].AutoPlay {
		t.Fatalf("等待房间恢复状态错误：%+v", restored)
	}
}

// TestManagerRestoreSkipsCorruptSnapshot 验证单个坏快照不会阻断其它房间启动恢复。
func TestManagerRestoreSkipsCorruptSnapshot(t *testing.T) {
	repository := newMemorySnapshotRepository()
	repository.records["BROKEN"] = []byte(`{"schema_version":1,"room_id":"OTHER"}`)
	manager := NewManager(nil, Options{SnapshotRepository: repository})
	if err := manager.Restore(context.Background()); err != nil {
		t.Fatalf("坏快照不应阻断整体恢复：%v", err)
	}
	if manager.Count() != 0 || repository.deleteCount != 1 {
		t.Fatalf("坏快照未被隔离清理：count=%d deletes=%d", manager.Count(), repository.deleteCount)
	}
}

// TestManagerRestoreReturnsLoadFailure 验证 Redis 启动扫描失败会阻止服务带着空状态监听。
func TestManagerRestoreReturnsLoadFailure(t *testing.T) {
	repository := newMemorySnapshotRepository()
	repository.err = errors.New("模拟 Redis 不可用")
	manager := NewManager(nil, Options{SnapshotRepository: repository})
	if err := manager.Restore(context.Background()); err == nil {
		t.Fatal("Redis 加载失败后仍然恢复成功")
	}
}

// TestRoomSnapshotFailureDoesNotFailMailboxCommand 验证 Redis 故障只记恢复副本错误，不改写业务结果。
func TestRoomSnapshotFailureDoesNotFailMailboxCommand(t *testing.T) {
	repository := newMemorySnapshotRepository()
	repository.err = errors.New("模拟快照写入失败")
	target := &Room{
		ID:                 "FAIL01",
		MaxPlayers:         4,
		OwnerID:            1,
		Phase:              PhaseWaiting,
		Members:            []*Member{{PlayerID: 1, Nickname: "房主", Ready: true}},
		snapshotsStore:     repository,
		snapshotTimeout:    time.Second,
		snapshotTTL:        time.Hour,
		mailbox:            make(chan roomCommand, mailboxSize),
		closed:             make(chan struct{}),
		logger:             nil,
		turnTimeout:        time.Hour,
		managedActionDelay: time.Hour,
	}
	// loop 需要非空日志器，但测试不关心输出内容。
	target.logger = NewManager(nil, Options{}).logger
	go target.loop()
	if err := target.submitSync(context.Background(), commandSnapshot, nil, protocol.Envelope{}); err != nil {
		t.Fatalf("快照失败污染了 mailbox 命令结果：%v", err)
	}
	repository.err = nil
	if err := target.submitSync(context.Background(), commandStop, nil, protocol.Envelope{}); err != nil {
		t.Fatalf("停止测试房间失败：%v", err)
	}
}

// TestRoomSettledSnapshotDeletesRedisState 验证终局房间不再保存可恢复快照。
func TestRoomSettledSnapshotDeletesRedisState(t *testing.T) {
	repository := newMemorySnapshotRepository()
	target := &Room{
		ID:              "DONE01",
		Phase:           PhaseSettled,
		Members:         []*Member{{PlayerID: 1, Nickname: "胜者"}, {PlayerID: 2, Nickname: "玩家乙"}},
		snapshotsStore:  repository,
		snapshotTimeout: time.Second,
		snapshotTTL:     time.Hour,
	}
	if err := target.syncSnapshot(context.Background()); err != nil {
		t.Fatalf("删除终局快照失败：%v", err)
	}
	if repository.deleteCount != 1 || repository.saveCount != 0 {
		t.Fatalf("终局快照行为错误：save=%d delete=%d", repository.saveCount, repository.deleteCount)
	}
}

// memorySnapshotRepository 为房间恢复测试保存不透明 JSON，并记录刷新与清理次数。
type memorySnapshotRepository struct {
	mu          sync.Mutex
	records     map[string][]byte
	err         error
	saveCount   int
	deleteCount int
}

// newMemorySnapshotRepository 创建空的线程安全快照仓储。
func newMemorySnapshotRepository() *memorySnapshotRepository {
	return &memorySnapshotRepository{records: make(map[string][]byte)}
}

// put 把结构化快照编码后放入测试仓储。
func (r *memorySnapshotRepository) put(t *testing.T, snapshot roomSnapshot) {
	t.Helper()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("编码测试快照失败：%v", err)
	}
	r.records[snapshot.RoomID] = payload
}

// SaveRoomSnapshot 保存房间 JSON，并记录恢复后的立即刷新。
func (r *memorySnapshotRepository) SaveRoomSnapshot(
	_ context.Context,
	roomID string,
	_ []int64,
	payload []byte,
	_ time.Duration,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.records[roomID] = append([]byte(nil), payload...)
	r.saveCount++
	return nil
}

// DeleteRoomSnapshot 删除测试快照并记录清理次数。
func (r *memorySnapshotRepository) DeleteRoomSnapshot(_ context.Context, roomID string, _ []int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	delete(r.records, roomID)
	r.deleteCount++
	return nil
}

// DeletePlayerRoom 在该内存实现中无需维护额外索引。
func (r *memorySnapshotRepository) DeletePlayerRoom(_ context.Context, _ string, _ int64) error {
	return r.err
}

// LoadRoomSnapshots 按房间号返回深拷贝，模拟生产仓储的稳定顺序。
func (r *memorySnapshotRepository) LoadRoomSnapshots(_ context.Context) ([]store.RoomSnapshotRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	roomIDs := make([]string, 0, len(r.records))
	for roomID := range r.records {
		roomIDs = append(roomIDs, roomID)
	}
	sort.Strings(roomIDs)
	records := make([]store.RoomSnapshotRecord, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		records = append(records, store.RoomSnapshotRecord{
			RoomID:  roomID,
			Payload: append([]byte(nil), r.records[roomID]...),
		})
	}
	return records, nil
}
