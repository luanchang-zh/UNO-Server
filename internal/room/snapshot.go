package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// roomSnapshotSchemaVersion 标识房间层 JSON 结构，与 UNO 引擎快照版本独立演进。
const roomSnapshotSchemaVersion = 1

// errSettledSnapshot 表示无需恢复、只需清理的历史终局记录。
var errSettledSnapshot = errors.New("房间快照已经结算")

// memberSnapshot 保存房间座位的业务状态，不序列化进程内 Session 指针。
type memberSnapshot struct {
	// PlayerID 是座位绑定的稳定玩家 ID。
	PlayerID int64 `json:"player_id"`
	// Nickname 是进入房间时的展示昵称。
	Nickname string `json:"nickname"`
	// Ready 表示成员是否满足开局准备条件。
	Ready bool `json:"ready"`
	// Connected 仅记录快照时状态；恢复时统一重置为 false。
	Connected bool `json:"connected"`
	// AutoPlay 表示成员是否已经进入托管。
	AutoPlay bool `json:"auto_play"`
	// TimeoutStrikes 是当前连续超时次数。
	TimeoutStrikes int `json:"timeout_strikes"`
	// Abandoned 表示成员主动离局且不再允许重连。
	Abandoned bool `json:"abandoned"`
}

// roomSnapshot 是 Redis 中完整且带版本的房间权威状态。
type roomSnapshot struct {
	// SchemaVersion 用于拒绝与当前代码不兼容的 JSON 结构。
	SchemaVersion int `json:"schema_version"`
	// Revision 是该房间单调递增的快照序号。
	Revision uint64 `json:"revision"`
	// UpdatedAt 是生成本快照的 UTC 时间。
	UpdatedAt time.Time `json:"updated_at"`
	// RoomID 是房间唯一编号，并会与 Redis 键中的编号交叉校验。
	RoomID string `json:"room_id"`
	// MaxPlayers 是房间人数上限。
	MaxPlayers int `json:"max_players"`
	// OwnerID 是当前房主玩家 ID。
	OwnerID int64 `json:"owner_id"`
	// Phase 是 waiting、playing 或 settled。
	Phase string `json:"phase"`
	// Members 按稳定座位顺序保存全部成员。
	Members []memberSnapshot `json:"members"`
	// Engine 是进行中牌局的完整私有状态；等待阶段为空。
	Engine *uno.Snapshot `json:"engine,omitempty"`
	// TurnDeadline 是当前行动计时器的绝对截止时间。
	TurnDeadline *time.Time `json:"turn_deadline,omitempty"`
	// MatchID 是 MySQL 中与当前牌局对应的主键；纯内存模式下可为零。
	MatchID int64 `json:"match_id,omitempty"`
	// MatchStartedAt 是 MySQL 对局的 UTC 开始时间。
	MatchStartedAt *time.Time `json:"match_started_at,omitempty"`
	// MatchSettled 防止同一进程内重复提交终局结果。
	MatchSettled bool `json:"match_settled"`
}

// syncSnapshot 在 mailbox 内同步保存当前权威状态；终局或销毁房间改为删除快照。
func (r *Room) syncSnapshot(parent context.Context) error {
	if r.snapshotsStore == nil {
		return nil
	}
	ctx, cancel := r.snapshotContext(parent)
	defer cancel()
	if r.destroyed || r.Phase == PhaseSettled {
		return r.snapshotsStore.DeleteRoomSnapshot(ctx, r.ID, r.allSnapshotPlayerIDs())
	}

	snapshot := r.buildSnapshot()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode room snapshot %s: %w", r.ID, err)
	}
	return r.snapshotsStore.SaveRoomSnapshot(
		ctx,
		r.ID,
		r.activeSnapshotPlayerIDs(),
		payload,
		r.snapshotTTL,
	)
}

// buildSnapshot 深拷贝房间和引擎状态，并生成新的单调修订号。
func (r *Room) buildSnapshot() roomSnapshot {
	r.snapshotRevision++
	now := time.Now().UTC()
	snapshot := roomSnapshot{
		SchemaVersion: roomSnapshotSchemaVersion,
		Revision:      r.snapshotRevision,
		UpdatedAt:     now,
		RoomID:        r.ID,
		MaxPlayers:    r.MaxPlayers,
		OwnerID:       r.OwnerID,
		Phase:         r.Phase,
		Members:       make([]memberSnapshot, 0, len(r.Members)),
		MatchID:       r.matchID,
		MatchSettled:  r.matchSettled,
	}
	for _, member := range r.Members {
		snapshot.Members = append(snapshot.Members, memberSnapshot{
			PlayerID:       member.PlayerID,
			Nickname:       member.Nickname,
			Ready:          member.Ready,
			Connected:      member.Connected,
			AutoPlay:       member.AutoPlay,
			TimeoutStrikes: member.TimeoutStrikes,
			Abandoned:      member.abandoned,
		})
	}
	if r.engine != nil {
		engineSnapshot := r.engine.Snapshot()
		snapshot.Engine = &engineSnapshot
	}
	if !r.turnDeadline.IsZero() {
		deadline := r.turnDeadline.UTC()
		snapshot.TurnDeadline = &deadline
	}
	if !r.matchStartedAt.IsZero() {
		startedAt := r.matchStartedAt.UTC()
		snapshot.MatchStartedAt = &startedAt
	}
	return snapshot
}

// activeSnapshotPlayerIDs 返回仍允许通过 token 重连的成员 ID。
func (r *Room) activeSnapshotPlayerIDs() []int64 {
	playerIDs := make([]int64, 0, len(r.Members))
	for _, member := range r.Members {
		if !member.abandoned {
			playerIDs = append(playerIDs, member.PlayerID)
		}
	}
	return playerIDs
}

// allSnapshotPlayerIDs 返回所有座位 ID，用于终局时清理可能残留的重连索引。
func (r *Room) allSnapshotPlayerIDs() []int64 {
	playerIDs := make([]int64, 0, len(r.Members))
	for _, member := range r.Members {
		playerIDs = append(playerIDs, member.PlayerID)
	}
	return playerIDs
}

// snapshotContext 为 Redis 快照操作附加统一超时并保留上游取消信号。
func (r *Room) snapshotContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, r.snapshotTimeout)
}

// restoreRoom 校验 JSON 与 UNO 引擎快照，并重建一间尚未对外发布的房间。
func restoreRoom(
	record store.RoomSnapshotRecord,
	logger *logx.Logger,
	options Options,
	hooks roomHooks,
) (*Room, error) {
	var snapshot roomSnapshot
	if err := json.Unmarshal(record.Payload, &snapshot); err != nil {
		return nil, fmt.Errorf("decode room snapshot %s: %w", record.RoomID, err)
	}
	if err := validateRoomSnapshot(record.RoomID, snapshot); err != nil {
		return nil, err
	}
	if snapshot.Phase == PhaseSettled {
		return nil, errSettledSnapshot
	}
	var engine *uno.Engine
	if snapshot.Engine != nil {
		restored, err := uno.Restore(*snapshot.Engine, uno.Config{})
		if err != nil {
			return nil, fmt.Errorf("restore UNO engine for room %s: %w", record.RoomID, err)
		}
		engine = restored
	}
	if logger == nil {
		logger = logx.NewFromSlog(nil)
	}

	room := &Room{
		ID:                 snapshot.RoomID,
		MaxPlayers:         snapshot.MaxPlayers,
		OwnerID:            snapshot.OwnerID,
		Phase:              snapshot.Phase,
		Members:            make([]*Member, 0, len(snapshot.Members)),
		engine:             engine,
		turnTimeout:        options.TurnTimeout,
		managedActionDelay: options.ManagedActionDelay,
		timeoutStrikeLimit: options.TimeoutStrikeLimit,
		idGenerator:        options.IDGenerator,
		matchesStore:       options.MatchRepository,
		persistTimeout:     options.PersistenceTimeout,
		matchID:            snapshot.MatchID,
		matchSettled:       snapshot.MatchSettled,
		snapshotsStore:     options.SnapshotRepository,
		snapshotTimeout:    options.SnapshotTimeout,
		snapshotTTL:        options.SnapshotTTL,
		snapshotRevision:   snapshot.Revision,
		mailbox:            make(chan roomCommand, mailboxSize),
		closed:             make(chan struct{}),
		logger:             logger,
		onEmpty:            hooks.onEmpty,
		onMemberRemoved:    hooks.onMemberRemoved,
	}
	if snapshot.MatchStartedAt != nil {
		room.matchStartedAt = snapshot.MatchStartedAt.UTC()
	}
	for _, savedMember := range snapshot.Members {
		autoPlay := savedMember.AutoPlay
		if snapshot.Phase == PhasePlaying {
			autoPlay = true
		}
		room.Members = append(room.Members, &Member{
			PlayerID:       savedMember.PlayerID,
			Nickname:       savedMember.Nickname,
			Ready:          savedMember.Ready,
			Connected:      false,
			AutoPlay:       autoPlay,
			TimeoutStrikes: savedMember.TimeoutStrikes,
			Session:        nil,
			abandoned:      savedMember.Abandoned,
		})
	}
	if snapshot.Phase == PhasePlaying {
		room.scheduleRecoveredTurn(snapshot.TurnDeadline)
	}
	go room.loop()
	return room, nil
}

// validateRoomSnapshot 校验房间结构、成员身份和 UNO 座位顺序的一致性。
func validateRoomSnapshot(recordRoomID string, snapshot roomSnapshot) error {
	if snapshot.SchemaVersion != roomSnapshotSchemaVersion {
		return fmt.Errorf("room snapshot schema version %d: %w", snapshot.SchemaVersion, errs.ErrInvalidArgument)
	}
	if snapshot.Revision == 0 || snapshot.UpdatedAt.IsZero() {
		return fmt.Errorf("room snapshot revision or updated_at is empty: %w", errs.ErrInvalidArgument)
	}
	if strings.TrimSpace(snapshot.RoomID) == "" || snapshot.RoomID != recordRoomID {
		return fmt.Errorf("room snapshot id mismatch: %w", errs.ErrInvalidArgument)
	}
	if snapshot.MaxPlayers < minPlayers || snapshot.MaxPlayers > maxPlayersCap ||
		len(snapshot.Members) == 0 || len(snapshot.Members) > snapshot.MaxPlayers {
		return fmt.Errorf("room snapshot member count is invalid: %w", errs.ErrInvalidArgument)
	}
	if snapshot.Phase != PhaseWaiting && snapshot.Phase != PhasePlaying && snapshot.Phase != PhaseSettled {
		return fmt.Errorf("room snapshot phase %q is invalid: %w", snapshot.Phase, errs.ErrInvalidArgument)
	}
	seen := make(map[int64]struct{}, len(snapshot.Members))
	ownerFound := false
	for _, member := range snapshot.Members {
		if member.PlayerID <= 0 || strings.TrimSpace(member.Nickname) == "" || member.TimeoutStrikes < 0 {
			return fmt.Errorf("room snapshot member is invalid: %w", errs.ErrInvalidArgument)
		}
		if _, duplicate := seen[member.PlayerID]; duplicate {
			return fmt.Errorf("room snapshot player %d duplicated: %w", member.PlayerID, errs.ErrInvalidArgument)
		}
		seen[member.PlayerID] = struct{}{}
		ownerFound = ownerFound || member.PlayerID == snapshot.OwnerID
	}
	if !ownerFound {
		return fmt.Errorf("room snapshot owner is missing: %w", errs.ErrInvalidArgument)
	}
	if snapshot.Phase == PhaseWaiting {
		if snapshot.Engine != nil || snapshot.MatchID != 0 || snapshot.MatchStartedAt != nil {
			return fmt.Errorf("waiting room snapshot contains a match: %w", errs.ErrInvalidArgument)
		}
		for _, member := range snapshot.Members {
			if member.AutoPlay || member.Abandoned {
				return fmt.Errorf("waiting room snapshot contains managed member: %w", errs.ErrInvalidArgument)
			}
		}
		return nil
	}
	if len(snapshot.Members) < minPlayers || snapshot.Engine == nil {
		return fmt.Errorf("active room snapshot has no complete engine: %w", errs.ErrInvalidArgument)
	}
	if len(snapshot.Engine.Players) != len(snapshot.Members) {
		return fmt.Errorf("room and engine player count mismatch: %w", errs.ErrInvalidArgument)
	}
	for seat, player := range snapshot.Engine.Players {
		if player.PlayerID != snapshot.Members[seat].PlayerID {
			return fmt.Errorf("room and engine seat %d mismatch: %w", seat, errs.ErrInvalidArgument)
		}
	}
	if snapshot.MatchID < 0 || (snapshot.MatchID > 0 && snapshot.MatchStartedAt == nil) {
		return fmt.Errorf("room snapshot match metadata is invalid: %w", errs.ErrInvalidArgument)
	}
	if snapshot.Phase == PhasePlaying && snapshot.MatchSettled {
		return fmt.Errorf("playing room snapshot is already settled: %w", errs.ErrInvalidArgument)
	}
	return nil
}
