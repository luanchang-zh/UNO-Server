package uno

import (
	"errors"
	"testing"
	"time"
)

// TestPlayableCardsAndIllegalPlay 验证牌面匹配规则，以及拒绝非法出牌时状态不发生变化。
func TestPlayableCardsAndIllegalPlay(t *testing.T) {
	top := testCard(1, ColorRed, KindNumber, 5)
	redSeven := testCard(2, ColorRed, KindNumber, 7)
	blueFive := testCard(3, ColorBlue, KindNumber, 5)
	blueSkip := testCard(4, ColorBlue, KindSkip, 0)
	wild := testCard(5, ColorNone, KindWild, 0)
	engine := newActionTestEngine(
		[][]Card{{redSeven, blueFive, blueSkip, wild}, {testCard(6, ColorGreen, KindNumber, 1)}},
		nil,
		[]Card{top},
	)

	playable, err := engine.PlayableCards(1)
	if err != nil {
		t.Fatalf("PlayableCards() error: %v", err)
	}
	wantIDs := map[CardID]bool{redSeven.ID: true, blueFive.ID: true, wild.ID: true}
	if len(playable) != len(wantIDs) {
		t.Fatalf("playable=%v", playable)
	}
	for _, card := range playable {
		if !wantIDs[card.ID] {
			t.Errorf("unexpected playable card: %+v", card)
		}
	}

	beforeHand := cloneCards(engine.players[0].hand)
	if err := engine.Play(1, blueSkip.ID, false); !errors.Is(err, ErrIllegalCard) {
		t.Fatalf("Play() error=%v, want ErrIllegalCard", err)
	}
	if len(engine.players[0].hand) != len(beforeHand) || len(engine.discardPile) != 1 || engine.currentSeat != 0 {
		t.Fatal("illegal play mutated engine state")
	}
	for index := range beforeHand {
		if engine.players[0].hand[index] != beforeHand[index] {
			t.Fatal("illegal play changed hand contents")
		}
	}
}

// TestActionCardTurnTransitions 覆盖跳过牌和反转牌对座位轮转的影响。
func TestActionCardTurnTransitions(t *testing.T) {
	tests := []struct {
		name          string
		playerCount   int
		kind          Kind
		wantCurrent   int
		wantDirection Direction
	}{
		{name: "skip three", playerCount: 3, kind: KindSkip, wantCurrent: 2, wantDirection: DirectionClockwise},
		{name: "reverse three", playerCount: 3, kind: KindReverse, wantCurrent: 2, wantDirection: DirectionCounterClockwise},
		{name: "reverse two is skip", playerCount: 2, kind: KindReverse, wantCurrent: 0, wantDirection: DirectionClockwise},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hands := make([][]Card, test.playerCount)
			hands[0] = []Card{
				testCard(2, ColorRed, test.kind, 0),
				testCard(3, ColorBlue, KindNumber, 1),
			}
			for seat := 1; seat < test.playerCount; seat++ {
				hands[seat] = []Card{testCard(CardID(10+seat), ColorGreen, KindNumber, seat)}
			}
			engine := newActionTestEngine(hands, nil, []Card{testCard(1, ColorRed, KindNumber, 5)})
			if err := engine.Play(1, 2, true); err != nil {
				t.Fatalf("Play() error: %v", err)
			}
			if engine.currentSeat != test.wantCurrent || engine.direction != test.wantDirection {
				t.Fatalf("current=%d direction=%d", engine.currentSeat, engine.direction)
			}
		})
	}
}

// TestDrawPenaltyStacking 验证加二与加四可以跨类型叠加并一次性接受罚牌。
func TestDrawPenaltyStacking(t *testing.T) {
	engine := newActionTestEngine(
		[][]Card{
			{testCard(2, ColorRed, KindDrawTwo, 0), testCard(3, ColorBlue, KindNumber, 1)},
			{testCard(4, ColorNone, KindWildDrawFour, 0), testCard(5, ColorYellow, KindNumber, 2)},
			{testCard(6, ColorGreen, KindNumber, 3)},
		},
		[]Card{
			testCard(10, ColorRed, KindNumber, 1),
			testCard(11, ColorRed, KindNumber, 2),
			testCard(12, ColorRed, KindNumber, 3),
			testCard(13, ColorBlue, KindNumber, 4),
			testCard(14, ColorBlue, KindNumber, 5),
			testCard(15, ColorBlue, KindNumber, 6),
		},
		[]Card{testCard(1, ColorRed, KindNumber, 5)},
	)

	if err := engine.Play(1, 2, true); err != nil {
		t.Fatalf("play +2: %v", err)
	}
	if engine.pendingDraw != 2 || engine.currentSeat != 1 {
		t.Fatalf("after +2 pending=%d current=%d", engine.pendingDraw, engine.currentSeat)
	}
	if err := engine.Play(2, 4, true); err != nil {
		t.Fatalf("stack +4: %v", err)
	}
	if engine.phase != PhaseAwaitingColor || engine.pendingDraw != 2 {
		t.Fatalf("before color phase=%s pending=%d", engine.phase, engine.pendingDraw)
	}
	if err := engine.ChooseColor(2, ColorGreen); err != nil {
		t.Fatalf("ChooseColor(): %v", err)
	}
	if engine.pendingDraw != 6 || engine.currentSeat != 2 || engine.currentColor != ColorGreen {
		t.Fatalf("after +4 pending=%d current=%d color=%s", engine.pendingDraw, engine.currentSeat, engine.currentColor)
	}

	result, err := engine.Draw(3)
	if err != nil {
		t.Fatalf("accept penalty: %v", err)
	}
	if !result.Penalty || !result.TurnEnded || len(result.Cards) != 6 {
		t.Fatalf("draw result=%+v", result)
	}
	if engine.pendingDraw != 0 || engine.currentSeat != 0 || len(engine.players[2].hand) != 7 {
		t.Fatalf("after acceptance pending=%d current=%d hand=%d", engine.pendingDraw, engine.currentSeat, len(engine.players[2].hand))
	}
}

// TestDrawTwoCanStackOnWildDrawFour 验证加二与加四在两个方向上都能相互叠加。
func TestDrawTwoCanStackOnWildDrawFour(t *testing.T) {
	engine := newActionTestEngine(
		[][]Card{
			{testCard(2, ColorNone, KindWildDrawFour, 0), testCard(3, ColorRed, KindNumber, 7)},
			{testCard(4, ColorBlue, KindDrawTwo, 0), testCard(5, ColorYellow, KindNumber, 1)},
		},
		nil,
		[]Card{testCard(1, ColorRed, KindNumber, 5)},
	)
	if err := engine.Play(1, 2, true); err != nil {
		t.Fatalf("Play +4: %v", err)
	}
	if err := engine.ChooseColor(1, ColorRed); err != nil {
		t.Fatalf("ChooseColor(): %v", err)
	}
	if err := engine.Play(2, 4, true); err != nil {
		t.Fatalf("stack +2: %v", err)
	}
	if engine.pendingDraw != 6 || engine.currentSeat != 0 {
		t.Fatalf("pending=%d current=%d", engine.pendingDraw, engine.currentSeat)
	}
}

// TestWildDrawFourIgnoresSameColorHand 验证本项目有意采用的规则差异：
// 即使手中仍有同色牌，也允许打出加四万能牌，不实现官方质疑流程。
func TestWildDrawFourIgnoresSameColorHand(t *testing.T) {
	engine := newActionTestEngine(
		[][]Card{
			{testCard(2, ColorNone, KindWildDrawFour, 0), testCard(3, ColorRed, KindNumber, 7)},
			{testCard(4, ColorBlue, KindNumber, 1)},
		},
		nil,
		[]Card{testCard(1, ColorRed, KindNumber, 5)},
	)
	if err := engine.Play(1, 2, true); err != nil {
		t.Fatalf("Play +4 while holding red: %v", err)
	}
}

// TestDrawDecisionRestrictsPlay 验证过牌前只能打出刚摸到且可出的那张实体牌。
func TestDrawDecisionRestrictsPlay(t *testing.T) {
	redSeven := testCard(2, ColorRed, KindNumber, 7)
	drawnBlueFive := testCard(4, ColorBlue, KindNumber, 5)
	engine := newActionTestEngine(
		[][]Card{
			{redSeven, testCard(3, ColorYellow, KindNumber, 1)},
			{testCard(5, ColorGreen, KindNumber, 3)},
		},
		[]Card{drawnBlueFive},
		[]Card{testCard(1, ColorRed, KindNumber, 5)},
	)

	result, err := engine.Draw(1)
	if err != nil {
		t.Fatalf("Draw() error: %v", err)
	}
	if !result.CanPlay || result.TurnEnded || engine.phase != PhaseAwaitingDrawDecision {
		t.Fatalf("draw result=%+v phase=%s", result, engine.phase)
	}
	if err := engine.Play(1, redSeven.ID, false); !errors.Is(err, ErrIllegalCard) {
		t.Fatalf("playing old hand card error=%v", err)
	}
	if err := engine.Pass(1); err != nil {
		t.Fatalf("Pass() error: %v", err)
	}
	if engine.phase != PhasePlaying || engine.currentSeat != 1 || len(engine.players[0].hand) != 3 {
		t.Fatalf("after pass phase=%s current=%d hand=%d", engine.phase, engine.currentSeat, len(engine.players[0].hand))
	}
}

// TestPlayDrawnCard 验证摸到可出牌后选择立即打出的成功路径。
func TestPlayDrawnCard(t *testing.T) {
	drawn := testCard(4, ColorBlue, KindNumber, 5)
	engine := newActionTestEngine(
		[][]Card{{testCard(2, ColorYellow, KindNumber, 1)}, {testCard(3, ColorGreen, KindNumber, 3)}},
		[]Card{drawn},
		[]Card{testCard(1, ColorRed, KindNumber, 5)},
	)
	if _, err := engine.Draw(1); err != nil {
		t.Fatalf("Draw() error: %v", err)
	}
	if err := engine.Play(1, drawn.ID, true); err != nil {
		t.Fatalf("Play drawn card: %v", err)
	}
	if engine.phase != PhasePlaying || engine.currentSeat != 1 || engine.topCard().ID != drawn.ID {
		t.Fatalf("phase=%s current=%d top=%+v", engine.phase, engine.currentSeat, engine.topCard())
	}
}

// TestWildRequiresColor 验证显式万能牌选色阶段及选色权限归属。
func TestWildRequiresColor(t *testing.T) {
	wild := testCard(2, ColorNone, KindWild, 0)
	engine := newActionTestEngine(
		[][]Card{{wild, testCard(3, ColorBlue, KindNumber, 1)}, {testCard(4, ColorGreen, KindNumber, 3)}},
		nil,
		[]Card{testCard(1, ColorRed, KindNumber, 5)},
	)
	if err := engine.Play(1, wild.ID, false); err != nil {
		t.Fatalf("Play wild: %v", err)
	}
	if engine.phase != PhaseAwaitingColor || engine.currentColor != ColorNone {
		t.Fatalf("phase=%s color=%q", engine.phase, engine.currentColor)
	}
	if err := engine.ChooseColor(2, ColorGreen); !errors.Is(err, ErrNotYourTurn) {
		t.Fatalf("wrong chooser error=%v", err)
	}
	if err := engine.ChooseColor(1, ColorNone); !errors.Is(err, ErrInvalidColor) {
		t.Fatalf("invalid color error=%v", err)
	}
	if err := engine.ChooseColor(1, ColorGreen); err != nil {
		t.Fatalf("ChooseColor() error: %v", err)
	}
	if engine.phase != PhasePlaying || engine.currentSeat != 1 || engine.currentColor != ColorGreen {
		t.Fatalf("phase=%s current=%d color=%s", engine.phase, engine.currentSeat, engine.currentColor)
	}
}

// TestUNOChallenge 覆盖抓罚、主动补喊和挑战超时行为。
func TestUNOChallenge(t *testing.T) {
	newEngine := func() (*Engine, *testClock) {
		engine := newActionTestEngine(
			[][]Card{
				{testCard(2, ColorRed, KindNumber, 7), testCard(3, ColorBlue, KindNumber, 1)},
				{testCard(4, ColorGreen, KindNumber, 3)},
			},
			[]Card{testCard(5, ColorYellow, KindNumber, 2), testCard(6, ColorGreen, KindNumber, 4)},
			[]Card{testCard(1, ColorRed, KindNumber, 5)},
		)
		clock := &testClock{now: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)}
		engine.now = clock.Now
		return engine, clock
	}

	t.Run("caught", func(t *testing.T) {
		engine, _ := newEngine()
		if err := engine.Play(1, 2, false); err != nil {
			t.Fatalf("Play() error: %v", err)
		}
		result, err := engine.CatchUNO(2, 1)
		if err != nil {
			t.Fatalf("CatchUNO() error: %v", err)
		}
		if len(result.Cards) != 2 || len(engine.players[0].hand) != 3 {
			t.Fatalf("result=%+v hand=%d", result, len(engine.players[0].hand))
		}
		if len(engine.unoChallenges) != 0 {
			t.Fatal("challenge was not cleared")
		}
	})

	t.Run("self call", func(t *testing.T) {
		engine, _ := newEngine()
		if err := engine.Play(1, 2, false); err != nil {
			t.Fatalf("Play() error: %v", err)
		}
		if err := engine.CallUNO(1); err != nil {
			t.Fatalf("CallUNO() error: %v", err)
		}
		if _, err := engine.CatchUNO(2, 1); !errors.Is(err, ErrNoUNOChallenge) {
			t.Fatalf("CatchUNO() error=%v", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		engine, clock := newEngine()
		if err := engine.Play(1, 2, false); err != nil {
			t.Fatalf("Play() error: %v", err)
		}
		clock.now = clock.now.Add(defaultChallengeWindow)
		if _, err := engine.CatchUNO(2, 1); !errors.Is(err, ErrNoUNOChallenge) {
			t.Fatalf("CatchUNO() error=%v", err)
		}
		if len(engine.unoChallenges) != 0 {
			t.Fatal("expired challenge was not removed")
		}
	})

	t.Run("timer expiry", func(t *testing.T) {
		engine, clock := newEngine()
		if err := engine.Play(1, 2, false); err != nil {
			t.Fatalf("Play() error: %v", err)
		}
		clock.now = clock.now.Add(defaultChallengeWindow)
		expired := engine.ExpireUNOChallenges()
		if len(expired) != 1 || expired[0] != 1 {
			t.Fatalf("expired=%v", expired)
		}
	})
}

// TestFinalDrawPenaltySettlesBeforeScoring 验证末牌加二必须先完成罚摸，
// 新摸到的牌随后会计入胜者得分。
func TestFinalDrawPenaltySettlesBeforeScoring(t *testing.T) {
	engine := newActionTestEngine(
		[][]Card{
			{testCard(2, ColorRed, KindDrawTwo, 0)},
			{testCard(3, ColorBlue, KindNumber, 9)},
		},
		[]Card{
			testCard(4, ColorGreen, KindNumber, 5),
			testCard(5, ColorYellow, KindNumber, 1),
		},
		[]Card{testCard(1, ColorRed, KindNumber, 5)},
	)
	if err := engine.Play(1, 2, false); err != nil {
		t.Fatalf("Play final +2: %v", err)
	}
	if engine.phase != PhasePlaying || len(engine.winnerCandidates) != 1 ||
		engine.winnerCandidates[0] != 0 || engine.pendingDraw != 2 {
		t.Fatalf("phase=%s candidates=%v pending=%d", engine.phase, engine.winnerCandidates, engine.pendingDraw)
	}
	if _, err := engine.Draw(2); err != nil {
		t.Fatalf("accept final penalty: %v", err)
	}
	if engine.phase != PhaseFinished || engine.result == nil {
		t.Fatalf("phase=%s result=%+v", engine.phase, engine.result)
	}
	if engine.result.WinnerID != 1 || engine.result.Score != 15 {
		t.Fatalf("result=%+v", engine.result)
	}
	if engine.result.Players[1].CardsLeft != 3 || engine.result.Players[1].HandPoints != 15 {
		t.Fatalf("loser result=%+v", engine.result.Players[1])
	}
}

// TestFinalPenaltyCanChangeWinner 验证胜者只能在末牌叠罚全部完成后确定：
// 候选玩家若被迫重新摸牌，将退出胜者候选列表。
func TestFinalPenaltyCanChangeWinner(t *testing.T) {
	engine := newActionTestEngine(
		[][]Card{
			{testCard(2, ColorRed, KindDrawTwo, 0)},
			{testCard(3, ColorBlue, KindDrawTwo, 0)},
		},
		[]Card{
			testCard(4, ColorGreen, KindNumber, 1),
			testCard(5, ColorGreen, KindNumber, 2),
			testCard(6, ColorYellow, KindNumber, 3),
			testCard(7, ColorYellow, KindNumber, 4),
		},
		[]Card{testCard(1, ColorRed, KindNumber, 5)},
	)
	if err := engine.Play(1, 2, false); err != nil {
		t.Fatalf("player 1 final +2: %v", err)
	}
	if err := engine.Play(2, 3, false); err != nil {
		t.Fatalf("player 2 final +2 stack: %v", err)
	}
	if _, err := engine.Draw(1); err != nil {
		t.Fatalf("player 1 accepts returned penalty: %v", err)
	}
	if engine.result == nil || engine.result.WinnerID != 2 || engine.result.Score != 10 {
		t.Fatalf("result=%+v", engine.result)
	}
}

// TestDrawPileRecycleKeepsDiscardTop 验证抽牌堆耗尽时保留弃牌堆顶的回收语义。
func TestDrawPileRecycleKeepsDiscardTop(t *testing.T) {
	redThree := testCard(1, ColorRed, KindNumber, 3)
	blueFour := testCard(2, ColorBlue, KindNumber, 4)
	engine := newActionTestEngine(
		[][]Card{{testCard(3, ColorYellow, KindNumber, 7)}, {testCard(4, ColorGreen, KindNumber, 8)}},
		nil,
		[]Card{redThree, blueFour},
	)
	result, err := engine.Draw(1)
	if err != nil {
		t.Fatalf("Draw() error: %v", err)
	}
	if len(result.Cards) != 1 || result.Cards[0].ID != redThree.ID || !result.TurnEnded {
		t.Fatalf("result=%+v", result)
	}
	if len(engine.discardPile) != 1 || engine.topCard().ID != blueFour.ID {
		t.Fatalf("discard pile=%v", engine.discardPile)
	}
}

// TestPenaltyDrawRollsBackOnShuffleError 验证回收洗牌失败时不会丢失已弹出的牌，
// 也不会留下只执行一部分的命令状态。
func TestPenaltyDrawRollsBackOnShuffleError(t *testing.T) {
	drawCard := testCard(3, ColorYellow, KindNumber, 7)
	bottomDiscard := testCard(1, ColorRed, KindNumber, 3)
	topDiscard := testCard(2, ColorBlue, KindNumber, 4)
	engine := newActionTestEngine(
		[][]Card{{testCard(4, ColorGreen, KindNumber, 1)}, {testCard(5, ColorGreen, KindNumber, 8)}},
		[]Card{drawCard},
		[]Card{bottomDiscard, topDiscard},
	)
	engine.pendingDraw = 2
	engine.shuffle = func(_ []Card) error { return errors.New("shuffle failed") }

	if _, err := engine.Draw(1); err == nil {
		t.Fatal("Draw() should return the shuffle error")
	}
	if engine.pendingDraw != 2 || engine.currentSeat != 0 || len(engine.players[0].hand) != 1 {
		t.Fatalf("pending=%d current=%d hand=%d", engine.pendingDraw, engine.currentSeat, len(engine.players[0].hand))
	}
	if len(engine.drawPile) != 1 || engine.drawPile[0] != drawCard {
		t.Fatalf("draw pile=%v", engine.drawPile)
	}
	if len(engine.discardPile) != 2 ||
		engine.discardPile[0] != bottomDiscard || engine.discardPile[1] != topDiscard {
		t.Fatalf("discard pile=%v", engine.discardPile)
	}
}
