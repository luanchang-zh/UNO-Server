package protocol

// 房间相关消息类型（M1）。
const (
	// TypeCreateRoom 客户端创建房间。
	TypeCreateRoom = "create_room"
	// TypeJoinRoom 客户端加入房间。
	TypeJoinRoom = "join_room"
	// TypeLeaveRoom 客户端离开房间。
	TypeLeaveRoom = "leave_room"
	// TypeReady 客户端准备/取消准备。
	TypeReady = "ready"
	// TypeStart 房主开始游戏。
	TypeStart = "start"
	// TypeKick 房主踢人（仅未开局）。
	TypeKick = "kick"
	// TypeRoomState 服务端推送房间快照。
	TypeRoomState = "room_state"
)

// CreateRoomPayload 创建房间请求。
type CreateRoomPayload struct {
	// MaxPlayers 人数上限 2–6，0 表示使用默认 4。
	MaxPlayers int `json:"max_players,omitempty"`
}

// JoinRoomPayload 加入房间请求。
type JoinRoomPayload struct {
	// RoomID 房间号。
	RoomID string `json:"room_id"`
}

// ReadyPayload 准备状态切换。
type ReadyPayload struct {
	// Ready true 准备，false 取消。
	Ready bool `json:"ready"`
}

// KickPayload 踢人请求。
type KickPayload struct {
	// PlayerID 被踢玩家。
	PlayerID int64 `json:"player_id"`
}

// RoomMemberView 房间成员公开视图。
type RoomMemberView struct {
	// PlayerID 是成员的稳定玩家 ID。
	PlayerID int64 `json:"player_id"`
	// Nickname 是成员显示昵称。
	Nickname string `json:"nickname"`
	// Ready 表示成员是否满足开局准备条件。
	Ready bool `json:"ready"`
	// IsOwner 表示成员是否为当前房主。
	IsOwner bool `json:"is_owner"`
	// Connected 表示成员当前是否绑定有效 WebSocket 会话。
	Connected bool `json:"connected"`
	// AutoPlay 表示成员当前是否由服务端自动托管。
	AutoPlay bool `json:"auto_play"`
	// TimeoutStrikes 是成员当前连续超时次数。
	TimeoutStrikes int `json:"timeout_strikes"`
}

// RoomStatePayload 房间状态推送体。
type RoomStatePayload struct {
	// RoomID 房间号。
	RoomID string `json:"room_id"`
	// Phase 是房间阶段，可取 waiting、playing 或 settled。
	Phase string `json:"phase"`
	// MaxPlayers 人数上限。
	MaxPlayers int `json:"max_players"`
	// OwnerID 房主 player_id。
	OwnerID int64 `json:"owner_id"`
	// Members 成员列表（座位顺序）。
	Members []RoomMemberView `json:"members"`
}
