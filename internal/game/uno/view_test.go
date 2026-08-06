package uno

import "testing"

// TestViewForProtectsOtherHands 验证集成视图会保护其他玩家手牌，且与实时状态分离。
func TestViewForProtectsOtherHands(t *testing.T) {
	engine, err := New([]int64{11, 22}, Config{Shuffle: noShuffle})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	view, err := engine.ViewFor(11)
	if err != nil {
		t.Fatalf("ViewFor() error: %v", err)
	}
	if len(view.Hand) != InitialHandSize || len(view.Players) != 2 {
		t.Fatalf("view hand=%d players=%d", len(view.Hand), len(view.Players))
	}
	if view.Players[1].Cards != InitialHandSize {
		t.Fatalf("other hand count=%d", view.Players[1].Cards)
	}

	original := view.Hand[0]
	view.Hand[0] = Card{}
	fresh, err := engine.ViewFor(11)
	if err != nil {
		t.Fatalf("ViewFor() error: %v", err)
	}
	if fresh.Hand[0] != original {
		t.Fatal("mutating view changed live hand")
	}
}

// TestViewForShowsOnlyCurrentPlayersLegalCards 验证界面提示不会向其他玩家泄露回合信息。
func TestViewForShowsOnlyCurrentPlayersLegalCards(t *testing.T) {
	engine, err := New([]int64{11, 22}, Config{Shuffle: noShuffle})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	nonCurrent, err := engine.ViewFor(11)
	if err != nil {
		t.Fatalf("ViewFor(non-current): %v", err)
	}
	if len(nonCurrent.PlayableCardIDs) != 0 {
		t.Fatalf("non-current playable IDs=%v", nonCurrent.PlayableCardIDs)
	}
	current, err := engine.ViewFor(22)
	if err != nil {
		t.Fatalf("ViewFor(current): %v", err)
	}
	if len(current.PlayableCardIDs) == 0 {
		t.Fatal("deterministic current hand should have at least one playable card")
	}
}
