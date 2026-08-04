// Package rediskey 定义 Redis 键模板与构造函数，避免魔法字符串散落各处。
//
// 实际读写在 store/redis；本包只负责键名约定与 TTL 语义说明。
package rediskey

import (
	"fmt"
	"time"
)

// 键前缀，统一加命名空间，避免与其它服务冲突。
const (
	// Prefix 为项目级前缀。
	Prefix = "uno"
)

// 默认 TTL（实现 store 时可引用；此处仅作约定）。
const (
	// DefaultSessionTTL 与登录 token 默认有效期对齐（24h）。
	DefaultSessionTTL = 24 * time.Hour
	// DefaultPlayerRoomTTL 玩家→房间索引的兜底过期（略长于一局长对局）。
	DefaultPlayerRoomTTL = 2 * time.Hour
	// DefaultRoomSnapshotTTL 房间快照兜底过期；活跃房间应持续续期。
	DefaultRoomSnapshotTTL = 2 * time.Hour
)

// Session 返回 token 会话键：uno:session:{token} → player 会话 JSON / player_id。
func Session(token string) string {
	return fmt.Sprintf("%s:session:%s", Prefix, token)
}

// PlayerRoom 返回玩家当前房间索引：uno:player_room:{playerID} → room_id。
func PlayerRoom(playerID int64) string {
	return fmt.Sprintf("%s:player_room:%d", Prefix, playerID)
}

// RoomSnapshot 返回房间热快照键：uno:room:{roomID} → 房间+牌局 JSON。
func RoomSnapshot(roomID string) string {
	return fmt.Sprintf("%s:room:%s", Prefix, roomID)
}

// RoomIndex 返回活跃房间 ID 集合键（SET），供进程启动扫描恢复。
func RoomIndex() string {
	return fmt.Sprintf("%s:room_index", Prefix)
}
