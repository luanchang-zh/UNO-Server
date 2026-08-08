package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/luanchang-zh/UNO-Server/internal/protocol"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// Restore 从 Redis 加载全部有效房间，并在对外监听前重建 mailbox 与玩家路由。
func (m *Manager) Restore(ctx context.Context) error {
	if m.options.SnapshotRepository == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.Count() != 0 {
		return fmt.Errorf("restore rooms requires an empty manager")
	}
	records, err := m.options.SnapshotRepository.LoadRoomSnapshots(ctx)
	if err != nil {
		return fmt.Errorf("load room snapshots: %w", err)
	}
	for _, record := range records {
		m.restoreRecord(ctx, record)
	}
	return nil
}

// restoreRecord 恢复单个快照；损坏、终局或身份冲突记录会被隔离并从 Redis 清理。
func (m *Manager) restoreRecord(ctx context.Context, record store.RoomSnapshotRecord) {
	hooks := roomHooks{onEmpty: m.removeRoom, onMemberRemoved: m.clearPlayerRoom}
	restored, err := restoreRoom(record, m.logger, m.options, hooks)
	if err != nil {
		levelMessage := "Redis 房间快照无效，已跳过恢复"
		if errors.Is(err, errSettledSnapshot) {
			levelMessage = "Redis 终局房间快照已清理"
		}
		m.logger.WithContext(ctx).Warn(levelMessage, "room_id", record.RoomID, "error", err)
		m.cleanupSnapshotRecord(ctx, record)
		return
	}

	m.mu.Lock()
	conflict := m.rooms[restored.ID] != nil
	if !conflict {
		for _, member := range restored.Members {
			if member.abandoned {
				continue
			}
			if existing := m.playerRoom[member.PlayerID]; existing != "" {
				conflict = true
				break
			}
		}
	}
	if !conflict {
		m.rooms[restored.ID] = restored
		for _, member := range restored.Members {
			if !member.abandoned {
				m.playerRoom[member.PlayerID] = restored.ID
			}
		}
	}
	m.mu.Unlock()
	if conflict {
		// 尚未发布的恢复房间先停止 mailbox，再清理由该停止动作最后写入的快照。
		_ = restored.submitSync(ctx, commandStop, nil, protocol.Envelope{})
		m.logger.WithContext(ctx).Warn("Redis 房间快照身份冲突，已跳过恢复", "room_id", record.RoomID)
		m.cleanupSnapshotRecord(ctx, record)
		return
	}

	// 恢复后立即覆盖 connected/auto_play 与新截止时间，kill -9 再次发生时仍可正确接续。
	_ = restored.submitSync(ctx, commandSnapshot, nil, protocol.Envelope{})
}

// cleanupSnapshotRecord 尽可能提取成员 ID，并条件清理房间及玩家 Redis 索引。
func (m *Manager) cleanupSnapshotRecord(ctx context.Context, record store.RoomSnapshotRecord) {
	playerIDs := snapshotPlayerIDs(record.Payload)
	cleanupCtx, cancel := context.WithTimeout(ctx, m.options.SnapshotTimeout)
	defer cancel()
	if err := m.options.SnapshotRepository.DeleteRoomSnapshot(cleanupCtx, record.RoomID, playerIDs); err != nil {
		m.logger.WithContext(cleanupCtx).Error(
			"Redis 无效房间快照清理失败",
			"room_id", record.RoomID,
			"error", err,
		)
	}
}

// snapshotPlayerIDs 从可能损坏的 JSON 中保守提取正数且不重复的成员 ID。
func snapshotPlayerIDs(payload []byte) []int64 {
	var minimal struct {
		Members []struct {
			PlayerID int64 `json:"player_id"`
		} `json:"members"`
	}
	if err := json.Unmarshal(payload, &minimal); err != nil {
		return nil
	}
	seen := make(map[int64]struct{}, len(minimal.Members))
	playerIDs := make([]int64, 0, len(minimal.Members))
	for _, member := range minimal.Members {
		if member.PlayerID <= 0 {
			continue
		}
		if _, duplicate := seen[member.PlayerID]; duplicate {
			continue
		}
		seen[member.PlayerID] = struct{}{}
		playerIDs = append(playerIDs, member.PlayerID)
	}
	return playerIDs
}

// Shutdown 要求每间房在自己的 mailbox 内刷最后快照并停止计时器。
func (m *Manager) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.RLock()
	rooms := make([]*Room, 0, len(m.rooms))
	for _, target := range m.rooms {
		rooms = append(rooms, target)
	}
	m.mu.RUnlock()

	var joined error
	for _, target := range rooms {
		if err := target.submitSync(ctx, commandStop, nil, protocol.Envelope{}); err != nil {
			joined = errors.Join(joined, fmt.Errorf("stop room %s: %w", target.ID, err))
		}
	}
	return joined
}
