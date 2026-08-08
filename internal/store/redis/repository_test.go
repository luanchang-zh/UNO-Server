package redis

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/model/rediskey"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// TestRepositorySessionLifecycle 验证 token JSON、TTL、回源读取和幂等删除。
func TestRepositorySessionLifecycle(t *testing.T) {
	repository, server := newTestRepository(t)
	ctx := context.Background()
	record := store.SessionRecord{
		PlayerID:  42,
		Nickname:  "墨鱼",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond),
	}
	if err := repository.SaveSession(ctx, "token-42", record, time.Hour); err != nil {
		t.Fatalf("保存会话失败：%v", err)
	}
	loaded, err := repository.FindSession(ctx, "token-42")
	if err != nil {
		t.Fatalf("读取会话失败：%v", err)
	}
	if loaded.PlayerID != record.PlayerID || loaded.Nickname != record.Nickname ||
		!loaded.ExpiresAt.Equal(record.ExpiresAt) {
		t.Fatalf("会话不一致：got=%+v want=%+v", loaded, record)
	}

	server.FastForward(time.Hour + time.Millisecond)
	if _, err := repository.FindSession(ctx, "token-42"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("TTL 到期后错误=%v", err)
	}
	if err := repository.DeleteSession(ctx, "token-42"); err != nil {
		t.Fatalf("重复删除会话失败：%v", err)
	}
}

// TestRepositoryRoomSnapshotLifecycle 验证房间原子索引、稳定加载与条件清理。
func TestRepositoryRoomSnapshotLifecycle(t *testing.T) {
	repository, server := newTestRepository(t)
	ctx := context.Background()
	if err := repository.SaveRoomSnapshot(ctx, "ROOM-B", []int64{2}, []byte(`{"room_id":"ROOM-B"}`), time.Hour); err != nil {
		t.Fatalf("保存 B 房失败：%v", err)
	}
	if err := repository.SaveRoomSnapshot(ctx, "ROOM-A", []int64{1}, []byte(`{"room_id":"ROOM-A"}`), time.Hour); err != nil {
		t.Fatalf("保存 A 房失败：%v", err)
	}
	if got, err := server.Get(rediskey.PlayerRoom(1)); err != nil || got != "ROOM-A" {
		t.Fatalf("玩家房间索引=%q err=%v", got, err)
	}

	records, err := repository.LoadRoomSnapshots(ctx)
	if err != nil {
		t.Fatalf("加载房间失败：%v", err)
	}
	if len(records) != 2 || records[0].RoomID != "ROOM-A" || records[1].RoomID != "ROOM-B" {
		t.Fatalf("房间加载顺序错误：%+v", records)
	}

	// 玩家 1 后来进入新房时，迟到的旧房清理不得删除新索引。
	if err := repository.SaveRoomSnapshot(ctx, "ROOM-C", []int64{1}, []byte(`{"room_id":"ROOM-C"}`), time.Hour); err != nil {
		t.Fatalf("保存 C 房失败：%v", err)
	}
	if err := repository.DeleteRoomSnapshot(ctx, "ROOM-A", []int64{1}); err != nil {
		t.Fatalf("删除 A 房失败：%v", err)
	}
	if got, err := server.Get(rediskey.PlayerRoom(1)); err != nil || got != "ROOM-C" {
		t.Fatalf("迟到清理误删新索引：got=%q err=%v", got, err)
	}
	if err := repository.DeletePlayerRoom(ctx, "ROOM-A", 1); err != nil {
		t.Fatalf("条件清理旧房索引失败：%v", err)
	}
	if got, _ := server.Get(rediskey.PlayerRoom(1)); got != "ROOM-C" {
		t.Fatalf("单玩家迟到清理误删新索引：%q", got)
	}
	if err := repository.DeleteRoomSnapshot(ctx, "ROOM-C", []int64{1}); err != nil {
		t.Fatalf("删除 C 房失败：%v", err)
	}
	if server.Exists(rediskey.PlayerRoom(1)) {
		t.Fatal("当前房间删除后玩家索引仍存在")
	}
}

// TestRepositoryLoadRoomSnapshotsCleansStaleIndex 验证启动扫描会移除失去值的集合成员。
func TestRepositoryLoadRoomSnapshotsCleansStaleIndex(t *testing.T) {
	repository, server := newTestRepository(t)
	if _, err := server.SAdd(rediskey.RoomIndex(), "MISSING"); err != nil {
		t.Fatalf("准备悬空索引失败：%v", err)
	}
	records, err := repository.LoadRoomSnapshots(context.Background())
	if err != nil {
		t.Fatalf("加载房间失败：%v", err)
	}
	stillIndexed := false
	if server.Exists(rediskey.RoomIndex()) {
		stillIndexed, err = server.SIsMember(rediskey.RoomIndex(), "MISSING")
		if err != nil {
			t.Fatalf("检查悬空索引失败：%v", err)
		}
	}
	if len(records) != 0 || stillIndexed {
		t.Fatalf("悬空索引未清理：records=%+v indexed=%v", records, stillIndexed)
	}
}

// TestRepositoryRejectsInvalidInput 验证 Redis 边界不会接受空键、重复玩家或非法连接参数。
func TestRepositoryRejectsInvalidInput(t *testing.T) {
	repository, _ := newTestRepository(t)
	ctx := context.Background()
	if err := repository.SaveSession(ctx, "", store.SessionRecord{}, time.Hour); err == nil {
		t.Fatal("空 token 未被拒绝")
	}
	if err := repository.SaveRoomSnapshot(ctx, "ROOM", []int64{1, 1}, []byte(`{}`), time.Hour); err == nil {
		t.Fatal("重复玩家未被拒绝")
	}
	if _, err := normalizeOptions(Options{Addr: "127.0.0.1:6379", PoolSize: 1, MinIdleConns: 2}); err == nil {
		t.Fatal("非法连接池参数未被拒绝")
	}
}

// newTestRepository 启动内存 Redis，并通过生产 Open 路径创建真实 go-redis 客户端。
func newTestRepository(t *testing.T) (*Repository, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	repository, err := Open(context.Background(), Options{Addr: server.Addr()})
	if err != nil {
		t.Fatalf("打开测试 Redis 失败：%v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	return repository, server
}
