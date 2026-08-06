package uno

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const (
	// StandardDeckSize 是本项目经典牌库的牌数。
	StandardDeckSize = 108
	// InitialHandSize 是每名玩家的初始手牌数。
	InitialHandSize = 7
)

// ShuffleFunc 对牌切片原地洗牌；测试可注入确定性实现，
// 生产环境默认使用密码学安全的 Fisher-Yates 洗牌。
type ShuffleFunc func(cards []Card) error

// NewStandardDeck 构造带稳定实体牌 ID 的经典 108 张牌库；
// 调用方可以自由修改返回的切片。
func NewStandardDeck() []Card {
	deck := make([]Card, 0, StandardDeckSize)
	nextID := CardID(1)
	appendCard := func(color Color, kind Kind, number int) {
		deck = append(deck, Card{ID: nextID, Color: color, Kind: kind, Number: number})
		nextID++
	}

	for _, color := range Colors() {
		// 每种颜色包含一个零、两套一至九，以及两套三种有色功能牌。
		appendCard(color, KindNumber, 0)
		for number := 1; number <= 9; number++ {
			appendCard(color, KindNumber, number)
			appendCard(color, KindNumber, number)
		}
		for copyIndex := 0; copyIndex < 2; copyIndex++ {
			appendCard(color, KindSkip, 0)
			appendCard(color, KindReverse, 0)
			appendCard(color, KindDrawTwo, 0)
		}
	}
	// 无色牌包含四张普通万能牌和四张加四万能牌。
	for copyIndex := 0; copyIndex < 4; copyIndex++ {
		appendCard(ColorNone, KindWild, 0)
		appendCard(ColorNone, KindWildDrawFour, 0)
	}
	return deck
}

// secureShuffle 执行无偏的原地 Fisher-Yates 洗牌。
func secureShuffle(cards []Card) error {
	for index := len(cards) - 1; index > 0; index-- {
		// 每轮从尚未固定的位置中等概率选取交换目标，避免取模偏差。
		randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(index+1)))
		if err != nil {
			return fmt.Errorf("shuffle card %d: %w", index, err)
		}
		swapIndex := int(randomIndex.Int64())
		cards[index], cards[swapIndex] = cards[swapIndex], cards[index]
	}
	return nil
}

// cloneCards 返回与原切片分离的副本，供视图和快照使用。
func cloneCards(cards []Card) []Card {
	if cards == nil {
		return nil
	}
	return append([]Card(nil), cards...)
}
