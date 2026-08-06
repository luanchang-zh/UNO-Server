package uno

import (
	"errors"
	"testing"
)

// TestNew_DealsAndStarts 验证确定性发牌和普通开局流程。
func TestNew_DealsAndStarts(t *testing.T) {
	engine, err := New([]int64{11, 22}, Config{DealerSeat: 0, Shuffle: noShuffle})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	state := engine.Snapshot()
	if len(state.Players[0].Hand) != InitialHandSize || len(state.Players[1].Hand) != InitialHandSize {
		t.Fatalf("hand sizes=%d,%d", len(state.Players[0].Hand), len(state.Players[1].Hand))
	}
	if len(state.DrawPile) != StandardDeckSize-2*InitialHandSize-1 {
		t.Fatalf("draw pile=%d", len(state.DrawPile))
	}
	if state.CurrentSeat != 1 || state.Phase != PhasePlaying {
		t.Fatalf("current seat=%d phase=%s", state.CurrentSeat, state.Phase)
	}
	if state.DiscardPile[len(state.DiscardPile)-1].Kind != KindNumber {
		t.Fatalf("opening card=%+v", state.DiscardPile[len(state.DiscardPile)-1])
	}
}

// TestNew_RejectsInvalidPlayers 验证玩家数量与身份唯一性约束。
func TestNew_RejectsInvalidPlayers(t *testing.T) {
	tests := []struct {
		name      string
		playerIDs []int64
	}{
		{name: "too few", playerIDs: []int64{1}},
		{name: "too many", playerIDs: []int64{1, 2, 3, 4, 5, 6, 7}},
		{name: "duplicate", playerIDs: []int64{1, 1}},
		{name: "zero ID", playerIDs: []int64{0, 2}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.playerIDs, Config{Shuffle: noShuffle})
			if !errors.Is(err, ErrInvalidPlayers) {
				t.Fatalf("error=%v, want ErrInvalidPlayers", err)
			}
		})
	}
}

// TestNew_OpeningEffects 覆盖所有特殊开局弃牌的状态迁移。
func TestNew_OpeningEffects(t *testing.T) {
	tests := []struct {
		name          string
		players       []int64
		kind          Kind
		wantCurrent   int
		wantDirection Direction
		wantPhase     Phase
		wantFirstHand int
	}{
		{
			name: "skip", players: []int64{1, 2, 3}, kind: KindSkip,
			wantCurrent: 2, wantDirection: DirectionClockwise, wantPhase: PhasePlaying, wantFirstHand: 7,
		},
		{
			name: "reverse three players", players: []int64{1, 2, 3}, kind: KindReverse,
			wantCurrent: 2, wantDirection: DirectionCounterClockwise, wantPhase: PhasePlaying, wantFirstHand: 7,
		},
		{
			name: "reverse two players is skip", players: []int64{1, 2}, kind: KindReverse,
			wantCurrent: 0, wantDirection: DirectionClockwise, wantPhase: PhasePlaying, wantFirstHand: 7,
		},
		{
			name: "draw two", players: []int64{1, 2, 3}, kind: KindDrawTwo,
			wantCurrent: 2, wantDirection: DirectionClockwise, wantPhase: PhasePlaying, wantFirstHand: 9,
		},
		{
			name: "wild chooses before play", players: []int64{1, 2, 3}, kind: KindWild,
			wantCurrent: 1, wantDirection: DirectionClockwise, wantPhase: PhaseAwaitingColor, wantFirstHand: 7,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			engine, err := New(test.players, Config{
				DealerSeat: 0,
				Shuffle:    openingCardShuffle(len(test.players), test.kind),
			})
			if err != nil {
				t.Fatalf("New() error: %v", err)
			}
			state := engine.Snapshot()
			if state.CurrentSeat != test.wantCurrent || state.Direction != test.wantDirection || state.Phase != test.wantPhase {
				t.Fatalf("current=%d direction=%d phase=%s", state.CurrentSeat, state.Direction, state.Phase)
			}
			firstSeat := 1
			if len(state.Players[firstSeat].Hand) != test.wantFirstHand {
				t.Fatalf("first player hand=%d, want %d", len(state.Players[firstSeat].Hand), test.wantFirstHand)
			}
		})
	}
}

// TestNew_RejectsOpeningWildDrawFour 验证开局加四万能牌会被放回，
// 并改用另一张合法牌作为首张弃牌。
func TestNew_RejectsOpeningWildDrawFour(t *testing.T) {
	engine, err := New([]int64{1, 2}, Config{
		Shuffle: openingCardShuffle(2, KindWildDrawFour),
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	snapshot := engine.Snapshot()
	top := snapshot.DiscardPile[len(snapshot.DiscardPile)-1]
	if top.Kind == KindWildDrawFour {
		t.Fatalf("opening Wild Draw Four was not rejected: %+v", top)
	}
	cardCount := len(snapshot.DrawPile) + len(snapshot.DiscardPile)
	for _, player := range snapshot.Players {
		cardCount += len(player.Hand)
	}
	if cardCount != StandardDeckSize {
		t.Fatalf("card count=%d, want %d", cardCount, StandardDeckSize)
	}
}
