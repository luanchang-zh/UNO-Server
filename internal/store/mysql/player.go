package mysql

import (
	"context"
	"fmt"

	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
)

const insertPlayerQuery = `INSERT INTO players (
    id, nickname, created_at, updated_at, last_login_at
) VALUES (?, ?, ?, ?, ?)`

// CreatePlayer 新增玩家；主键冲突或连接错误会完整返回给登录边界。
func (r *Repository) CreatePlayer(ctx context.Context, player entity.Player) error {
	if player.ID <= 0 {
		return fmt.Errorf("player id must be positive")
	}
	_, err := r.db.ExecContext(
		ctx,
		insertPlayerQuery,
		player.ID,
		player.Nickname,
		player.CreatedAt.UTC(),
		player.UpdatedAt.UTC(),
		player.LastLoginAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert player %d: %w", player.ID, err)
	}
	return nil
}
