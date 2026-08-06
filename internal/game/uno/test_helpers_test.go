package uno

import (
	"fmt"
	"time"
)

// testClock 是可手动推进的确定性测试时钟。
type testClock struct {
	now time.Time
}

// Now 返回测试中配置的固定时刻。
func (c *testClock) Now() time.Time {
	return c.now
}

// noShuffle 保持牌序不变，供确定性测试使用。
func noShuffle(_ []Card) error {
	return nil
}

// testCard 以简洁参数构造符合标准表示的测试牌。
func testCard(id CardID, color Color, kind Kind, number int) Card {
	return Card{ID: id, Color: color, Kind: kind, Number: number}
}

// newActionTestEngine 构造无需完整牌库的聚焦规则状态。
// 快照测试改用 New 创建真实牌库，以继续满足实体牌守恒约束。
func newActionTestEngine(hands [][]Card, drawPile []Card, discardPile []Card) *Engine {
	players := make([]playerState, len(hands))
	for seat, hand := range hands {
		players[seat] = playerState{id: int64(seat + 1), hand: cloneCards(hand)}
	}
	clock := &testClock{now: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)}
	top := discardPile[len(discardPile)-1]
	currentColor := top.Color
	if top.IsWild() {
		currentColor = ColorRed
	}
	return &Engine{
		players:         players,
		drawPile:        cloneCards(drawPile),
		discardPile:     cloneCards(discardPile),
		dealerSeat:      0,
		currentSeat:     0,
		direction:       DirectionClockwise,
		currentColor:    currentColor,
		phase:           PhasePlaying,
		unoChallenges:   make(map[int64]UNOChallenge),
		shuffle:         noShuffle,
		now:             clock.Now,
		challengeWindow: defaultChallengeWindow,
	}
}

// openingCardShuffle 把指定牌放到发牌后将成为首张弃牌的位置，
// 后续洗牌调用保持牌序不变。
func openingCardShuffle(playerCount int, kind Kind) ShuffleFunc {
	callCount := 0
	return func(cards []Card) error {
		if callCount > 0 {
			callCount++
			return nil
		}
		callCount++
		targetIndex := len(cards) - playerCount*InitialHandSize - 1
		cardIndex := -1
		for index, card := range cards {
			if card.Kind == kind {
				cardIndex = index
				break
			}
		}
		if cardIndex < 0 || targetIndex < 0 {
			return fmt.Errorf("cannot place opening kind %s", kind)
		}
		cards[targetIndex], cards[cardIndex] = cards[cardIndex], cards[targetIndex]
		return nil
	}
}
