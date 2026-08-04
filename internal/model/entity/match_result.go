package entity

import "time"

// MatchResult 对应表 match_results，一局中单个玩家的结算行。
type MatchResult struct {
	// ID 代理主键。
	ID int64 `json:"id" db:"id"`
	// MatchID 关联 matches.id。
	MatchID int64 `json:"match_id" db:"match_id"`
	// PlayerID 玩家 ID。
	PlayerID int64 `json:"player_id" db:"player_id"`
	// SeatIndex 座位序号 0–5。
	SeatIndex int8 `json:"seat_index" db:"seat_index"`
	// IsWinner 是否本局胜者。
	IsWinner bool `json:"is_winner" db:"is_winner"`
	// Score 本局得分：胜者记其余玩家手牌点之和，其他人记 0。
	Score int `json:"score" db:"score"`
	// HandPoints 结束时该玩家手牌点数（计分明细）。
	HandPoints int `json:"hand_points" db:"hand_points"`
	// CardsLeft 结束时剩余手牌张数。
	CardsLeft int8 `json:"cards_left" db:"cards_left"`
	// CreatedAt 写入时间（UTC）。
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// TableName 返回 MySQL 表名。
func (MatchResult) TableName() string {
	return "match_results"
}
