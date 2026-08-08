package mysql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
)

const createMatchQuery = `INSERT INTO matches (
    id, room_id, status, player_count, winner_player_id, started_at, ended_at, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

const finishMatchQuery = `UPDATE matches
SET status = ?, winner_player_id = ?, ended_at = ?
WHERE id = ? AND status = ?`

const insertMatchResultQuery = `INSERT INTO match_results (
    id, match_id, player_id, seat_index, is_winner, score, hand_points, cards_left, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

// CreateMatch 在开局广播前写入 playing 对局元数据。
func (r *Repository) CreateMatch(ctx context.Context, match entity.Match) error {
	if match.ID <= 0 || match.Status != entity.MatchStatusPlaying {
		return fmt.Errorf("playing match metadata is invalid")
	}
	_, err := r.db.ExecContext(
		ctx,
		createMatchQuery,
		match.ID,
		match.RoomID,
		match.Status,
		match.PlayerCount,
		match.WinnerPlayerID,
		match.StartedAt.UTC(),
		match.EndedAt,
		match.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("insert match %d: %w", match.ID, err)
	}
	return nil
}

// FinishMatch 在单一事务中更新对局终态并插入每个座位的结算行。
func (r *Repository) FinishMatch(ctx context.Context, match entity.Match, results []entity.MatchResult) error {
	if err := validateSettlement(match, results); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin match settlement: %w", err)
	}
	defer func() {
		// Commit 成功后 Rollback 返回 sql.ErrTxDone；无需覆盖真正的业务结果。
		_ = tx.Rollback()
	}()

	updateResult, err := tx.ExecContext(
		ctx,
		finishMatchQuery,
		match.Status,
		*match.WinnerPlayerID,
		match.EndedAt.UTC(),
		match.ID,
		entity.MatchStatusPlaying,
	)
	if err != nil {
		return fmt.Errorf("update match %d: %w", match.ID, err)
	}
	affected, err := updateResult.RowsAffected()
	if err != nil {
		return fmt.Errorf("read match %d affected rows: %w", match.ID, err)
	}
	if affected != 1 {
		return fmt.Errorf("match %d is missing or already settled", match.ID)
	}

	statement, err := tx.PrepareContext(ctx, insertMatchResultQuery)
	if err != nil {
		return fmt.Errorf("prepare match result insert: %w", err)
	}
	defer statement.Close()
	for _, result := range results {
		if _, err := statement.ExecContext(
			ctx,
			result.ID,
			result.MatchID,
			result.PlayerID,
			result.SeatIndex,
			result.IsWinner,
			result.Score,
			result.HandPoints,
			result.CardsLeft,
			result.CreatedAt.UTC(),
		); err != nil {
			return fmt.Errorf("insert match result %d: %w", result.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit match %d settlement: %w", match.ID, err)
	}
	return nil
}

// validateSettlement 在开启事务前校验终局元数据与逐玩家结果的一致性。
func validateSettlement(match entity.Match, results []entity.MatchResult) error {
	if match.ID <= 0 || match.Status != entity.MatchStatusFinished ||
		match.WinnerPlayerID == nil || match.EndedAt == nil {
		return fmt.Errorf("finished match metadata is invalid")
	}
	if len(results) < 2 || int(match.PlayerCount) != len(results) {
		return fmt.Errorf("match result count does not match player count")
	}
	seenPlayers := make(map[int64]struct{}, len(results))
	seenResultIDs := make(map[int64]struct{}, len(results))
	winnerCount := 0
	for seat, result := range results {
		if result.ID <= 0 || result.MatchID != match.ID || result.PlayerID <= 0 ||
			int(result.SeatIndex) != seat || result.Score < 0 || result.HandPoints < 0 || result.CardsLeft < 0 {
			return fmt.Errorf("match result at seat %d is invalid", seat)
		}
		if _, duplicate := seenResultIDs[result.ID]; duplicate {
			return fmt.Errorf("result id %d is duplicated", result.ID)
		}
		seenResultIDs[result.ID] = struct{}{}
		if _, duplicate := seenPlayers[result.PlayerID]; duplicate {
			return fmt.Errorf("player %d has duplicate match result", result.PlayerID)
		}
		seenPlayers[result.PlayerID] = struct{}{}
		if result.IsWinner {
			winnerCount++
			if result.PlayerID != *match.WinnerPlayerID {
				return fmt.Errorf("winner result does not match match metadata")
			}
		} else if result.Score != 0 {
			return fmt.Errorf("non-winner player %d has non-zero score", result.PlayerID)
		}
	}
	if winnerCount != 1 {
		return fmt.Errorf("match settlement must contain exactly one winner")
	}
	return nil
}
