package entity

import "time"

// MatchStatus 对局记录状态。
type MatchStatus string

const (
	// MatchStatusPlaying 对局进行中。
	MatchStatusPlaying MatchStatus = "playing"
	// MatchStatusFinished 正常结算结束。
	MatchStatusFinished MatchStatus = "finished"
	// MatchStatusAborted 异常中止（如房间销毁、不可恢复）。
	MatchStatusAborted MatchStatus = "aborted"
)

// Match 对应表 matches，一局对局的元数据。
type Match struct {
	// ID 主键，业务侧 match_id。
	ID int64 `json:"id" db:"id"`
	// RoomID 开局时所在业务房间号，便于排查。
	RoomID string `json:"room_id" db:"room_id"`
	// Status 对局状态。
	Status MatchStatus `json:"status" db:"status"`
	// PlayerCount 开局人数（2–6）。
	PlayerCount int8 `json:"player_count" db:"player_count"`
	// WinnerPlayerID 胜者；中止或未结束时为 nil。
	WinnerPlayerID *int64 `json:"winner_player_id,omitempty" db:"winner_player_id"`
	// StartedAt 开局时间（UTC）。
	StartedAt time.Time `json:"started_at" db:"started_at"`
	// EndedAt 结束时间；进行中为 nil。
	EndedAt *time.Time `json:"ended_at,omitempty" db:"ended_at"`
	// CreatedAt 行创建时间（UTC）。
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// TableName 返回 MySQL 表名。
func (Match) TableName() string {
	return "matches"
}
