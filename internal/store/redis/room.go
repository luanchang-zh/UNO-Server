package redis

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/model/rediskey"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// saveRoomScript 在一个 Redis 执行周期内覆盖快照、登记房间并续期所有玩家索引。
var saveRoomScript = goredis.NewScript(`
redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[3])
redis.call("SADD", KEYS[2], ARGV[1])
for index = 3, #KEYS do
    redis.call("SET", KEYS[index], ARGV[1], "PX", ARGV[3])
end
return 1
`)

// deleteRoomScript 删除房间数据，并通过值比较保护已经换到其它房间的玩家索引。
var deleteRoomScript = goredis.NewScript(`
redis.call("DEL", KEYS[1])
redis.call("SREM", KEYS[2], ARGV[1])
for index = 3, #KEYS do
    if redis.call("GET", KEYS[index]) == ARGV[1] then
        redis.call("DEL", KEYS[index])
    end
end
return 1
`)

// deletePlayerRoomScript 只删除仍指向调用方房间的单个玩家索引。
var deletePlayerRoomScript = goredis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
end
return 0
`)

// SaveRoomSnapshot 使用 Lua 在单个 Redis 实例内原子更新快照、房间集合和重连索引。
func (r *Repository) SaveRoomSnapshot(
	ctx context.Context,
	roomID string,
	playerIDs []int64,
	payload []byte,
	ttl time.Duration,
) error {
	roomID, keys, err := roomKeys(roomID, playerIDs)
	if err != nil {
		return err
	}
	if len(payload) == 0 || ttl <= 0 {
		return fmt.Errorf("redis 房间快照或 TTL 非法: %w", errs.ErrInvalidArgument)
	}
	ttlMilliseconds := ttl.Milliseconds()
	if ttlMilliseconds < 1 {
		ttlMilliseconds = 1
	}
	if err := saveRoomScript.Run(
		contextOrBackground(ctx),
		r.client,
		keys,
		roomID,
		payload,
		strconv.FormatInt(ttlMilliseconds, 10),
	).Err(); err != nil {
		return fmt.Errorf("save redis room snapshot %s: %w", roomID, err)
	}
	return nil
}

// DeleteRoomSnapshot 原子删除房间与集合索引，并只删除仍指向该房间的玩家索引。
func (r *Repository) DeleteRoomSnapshot(ctx context.Context, roomID string, playerIDs []int64) error {
	roomID, keys, err := roomKeys(roomID, playerIDs)
	if err != nil {
		return err
	}
	if err := deleteRoomScript.Run(contextOrBackground(ctx), r.client, keys, roomID).Err(); err != nil {
		return fmt.Errorf("delete redis room snapshot %s: %w", roomID, err)
	}
	return nil
}

// DeletePlayerRoom 通过值比较避免迟到的离房清理误删玩家后来加入的新房间。
func (r *Repository) DeletePlayerRoom(ctx context.Context, roomID string, playerID int64) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" || playerID <= 0 {
		return fmt.Errorf("redis 玩家房间索引参数非法: %w", errs.ErrInvalidArgument)
	}
	if err := deletePlayerRoomScript.Run(
		contextOrBackground(ctx),
		r.client,
		[]string{rediskey.PlayerRoom(playerID)},
		roomID,
	).Err(); err != nil {
		return fmt.Errorf("delete redis player room %d: %w", playerID, err)
	}
	return nil
}

// LoadRoomSnapshots 按 room_index 批量读取快照，并清除没有对应快照值的悬空成员。
func (r *Repository) LoadRoomSnapshots(ctx context.Context) ([]store.RoomSnapshotRecord, error) {
	ctx = contextOrBackground(ctx)
	roomIDs, err := r.client.SMembers(ctx, rediskey.RoomIndex()).Result()
	if err != nil {
		return nil, fmt.Errorf("load redis room index: %w", err)
	}
	if len(roomIDs) == 0 {
		return []store.RoomSnapshotRecord{}, nil
	}
	sort.Strings(roomIDs)
	keys := make([]string, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		keys = append(keys, rediskey.RoomSnapshot(roomID))
	}
	values, err := r.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("load redis room snapshots: %w", err)
	}

	records := make([]store.RoomSnapshotRecord, 0, len(roomIDs))
	staleIDs := make([]any, 0)
	for index, value := range values {
		if value == nil {
			staleIDs = append(staleIDs, roomIDs[index])
			continue
		}
		payload, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("redis room snapshot %s 类型非法", roomIDs[index])
		}
		records = append(records, store.RoomSnapshotRecord{
			RoomID:  roomIDs[index],
			Payload: append([]byte(nil), payload...),
		})
	}
	if len(staleIDs) > 0 {
		if err := r.client.SRem(ctx, rediskey.RoomIndex(), staleIDs...).Err(); err != nil {
			return nil, fmt.Errorf("clean stale redis room index: %w", err)
		}
	}
	return records, nil
}

// roomKeys 校验并构造 Lua 脚本使用的房间键与去重后的玩家键。
func roomKeys(roomID string, playerIDs []int64) (string, []string, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return "", nil, fmt.Errorf("redis room id 不能为空: %w", errs.ErrInvalidArgument)
	}
	keys := []string{rediskey.RoomSnapshot(roomID), rediskey.RoomIndex()}
	seen := make(map[int64]struct{}, len(playerIDs))
	for _, playerID := range playerIDs {
		if playerID <= 0 {
			return "", nil, fmt.Errorf("redis player id 必须为正数: %w", errs.ErrInvalidArgument)
		}
		if _, duplicate := seen[playerID]; duplicate {
			return "", nil, fmt.Errorf("redis player id %d 重复: %w", playerID, errs.ErrInvalidArgument)
		}
		seen[playerID] = struct{}{}
		keys = append(keys, rediskey.PlayerRoom(playerID))
	}
	return roomID, keys, nil
}
