// Package entity 定义需要持久化到 MySQL 的领域表结构（与表字段一一对应）。
package entity

import "time"

// Player 对应表 players，表示玩家长期身份。
type Player struct {
	// ID 主键，业务侧 player_id。
	ID int64 `json:"id" db:"id"`
	// Nickname 展示昵称，允许重名，最长 32 字符。
	Nickname string `json:"nickname" db:"nickname"`
	// CreatedAt 首次创建时间（UTC）。
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	// UpdatedAt 资料最近更新时间（UTC）。
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	// LastLoginAt 最近一次登录时间（UTC）。
	LastLoginAt time.Time `json:"last_login_at" db:"last_login_at"`
}

// TableName 返回 MySQL 表名。
func (Player) TableName() string {
	return "players"
}
