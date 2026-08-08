// Package store 定义业务层依赖的持久化端口，具体数据库实现位于子包。
package store

import (
	"context"

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
