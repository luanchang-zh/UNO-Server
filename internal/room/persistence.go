package room

import (
	"context"
	"fmt"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
)

// persistMatchStart 在牌局对外进入 playing 前写入对局元数据。
func (r *Room) persistMatchStart(parent context.Context, startedAt time.Time) error {
	r.matchID = 0
	r.matchStartedAt = time.Time{}
	r.matchSettled = false
	if r.matchesStore == nil {
		return nil
	}
	matchID, err := r.idGenerator.Next()
	if err != nil {
		return fmt.Errorf("generate match id: %w", err)
	}
	match := entity.Match{
		ID:          matchID,
		RoomID:      r.ID,
		Status:      entity.MatchStatusPlaying,
		PlayerCount: int8(len(r.Members)),
		StartedAt:   startedAt.UTC(),
		CreatedAt:   startedAt.UTC(),
	}
	ctx, cancel := r.persistenceContext(parent)
	defer cancel()
	if err := r.matchesStore.CreateMatch(ctx, match); err != nil {
		return fmt.Errorf("create match %d: %w", matchID, err)
	}
	r.matchID = matchID
	r.matchStartedAt = match.StartedAt
	return nil
}

// persistSettlement 将引擎终局结果转换为实体，并调用仓储原子写入。
func (r *Room) persistSettlement(result *uno.RoundResult) error {
	if r.matchesStore == nil || r.matchID == 0 || r.matchSettled || result == nil {
		return nil
	}
	endedAt := time.Now().UTC()
	winnerID := result.WinnerID
	match := entity.Match{
		ID:             r.matchID,
		RoomID:         r.ID,
		Status:         entity.MatchStatusFinished,
		PlayerCount:    int8(len(result.Players)),
		WinnerPlayerID: &winnerID,
		StartedAt:      r.matchStartedAt,
		EndedAt:        &endedAt,
		CreatedAt:      r.matchStartedAt,
	}
	results := make([]entity.MatchResult, 0, len(result.Players))
	for seat, playerResult := range result.Players {
		resultID, err := r.idGenerator.Next()
		if err != nil {
			return fmt.Errorf("generate result id for seat %d: %w", seat, err)
		}
		results = append(results, entity.MatchResult{
			ID:         resultID,
			MatchID:    r.matchID,
			PlayerID:   playerResult.PlayerID,
			SeatIndex:  int8(seat),
			IsWinner:   playerResult.IsWinner,
			Score:      playerResult.Score,
			HandPoints: playerResult.HandPoints,
			CardsLeft:  int8(playerResult.CardsLeft),
			CreatedAt:  endedAt,
		})
	}
	ctx, cancel := r.persistenceContext(context.Background())
	defer cancel()
	if err := r.matchesStore.FinishMatch(ctx, match, results); err != nil {
		return fmt.Errorf("finish match %d: %w", r.matchID, err)
	}
	r.matchSettled = true
	return nil
}

// persistenceContext 为数据库边界附加统一超时，同时保留上游取消信号。
func (r *Room) persistenceContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, r.persistTimeout)
}
