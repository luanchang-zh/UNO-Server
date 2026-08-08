// Package store 定义业务层依赖的持久化端口，具体数据库实现位于子包。
package store

import (
	"context"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
)

// PlayerRepository 持久化玩家长期身份。
type PlayerRepository interface {
	// CreatePlayer 新增一名玩家，主键和时间由业务层预先生成。
	CreatePlayer(ctx context.Context, player entity.Player) error
}

// MatchRepository 持久化对局元数据和不可变结算明细。
type MatchRepository interface {
	// CreateMatch 在牌局进入 playing 前写入对局元数据。
	CreateMatch(ctx context.Context, match entity.Match) error
	// FinishMatch 在一个事务内结束对局并写入全部玩家结果。
	FinishMatch(ctx context.Context, match entity.Match, results []entity.MatchResult) error
}

// SessionRecord 是 Redis 中保存的 token 会话值，不重复保存已经位于键中的 token。
type SessionRecord struct {
	// PlayerID 是 token 绑定的稳定玩家 ID。
	PlayerID int64 `json:"player_id"`
	// Nickname 是签发 token 时的昵称快照。
	Nickname string `json:"nickname"`
	// ExpiresAt 是 token 的绝对过期时间，统一使用 UTC。
	ExpiresAt time.Time `json:"expires_at"`
}

// SessionRepository 持久化可跨进程恢复的 token 会话。
type SessionRepository interface {
	// SaveSession 使用给定 TTL 保存或覆盖 token 会话。
	SaveSession(ctx context.Context, token string, record SessionRecord, ttl time.Duration) error
	// FindSession 读取 token 会话；不存在时返回 errs.ErrNotFound。
	FindSession(ctx context.Context, token string) (SessionRecord, error)
	// DeleteSession 幂等删除 token 会话。
	DeleteSession(ctx context.Context, token string) error
}

// RoomSnapshotRecord 是 Redis 房间索引中的一份不透明 JSON 快照。
// 具体结构和版本由 room 包负责，存储层只保证键与内容原样对应。
type RoomSnapshotRecord struct {
	// RoomID 是快照所属房间号。
	RoomID string
	// Payload 是房间层生成的完整权威状态 JSON。
	Payload []byte
}

// RoomSnapshotRepository 持久化房间快照及玩家到房间的重连索引。
type RoomSnapshotRepository interface {
	// SaveRoomSnapshot 原子保存房间 JSON、活跃房间索引和当前成员索引。
	SaveRoomSnapshot(
		ctx context.Context,
		roomID string,
		playerIDs []int64,
		payload []byte,
		ttl time.Duration,
	) error
	// DeleteRoomSnapshot 原子删除房间快照，并条件删除仍指向该房间的成员索引。
	DeleteRoomSnapshot(ctx context.Context, roomID string, playerIDs []int64) error
	// DeletePlayerRoom 条件删除仍指向指定房间的单个玩家索引。
	DeletePlayerRoom(ctx context.Context, roomID string, playerID int64) error
	// LoadRoomSnapshots 按房间号稳定排序加载当前活跃房间快照，并清理失效索引。
	LoadRoomSnapshots(ctx context.Context) ([]RoomSnapshotRecord, error)
}
