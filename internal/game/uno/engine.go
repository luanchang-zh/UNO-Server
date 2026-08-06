package uno

import "fmt"

// New 为 2 至 6 名不重复玩家创建、洗牌并发完一局游戏。
// 座位顺序与 playerIDs 的传入顺序一致。
func New(playerIDs []int64, config Config) (*Engine, error) {
	if err := validatePlayerIDs(playerIDs); err != nil {
		return nil, err
	}
	normalized, err := normalizedConfig(config, len(playerIDs))
	if err != nil {
		return nil, err
	}

	deck := NewStandardDeck()
	// 先完成洗牌再创建可对外返回的引擎，失败时不会暴露半初始化状态。
	if err := normalized.Shuffle(deck); err != nil {
		return nil, fmt.Errorf("initial shuffle: %w", err)
	}

	engine := &Engine{
		players:         make([]playerState, len(playerIDs)),
		drawPile:        deck,
		dealerSeat:      normalized.DealerSeat,
		currentSeat:     -1,
		direction:       DirectionClockwise,
		phase:           PhasePlaying,
		unoChallenges:   make(map[int64]UNOChallenge),
		shuffle:         normalized.Shuffle,
		now:             normalized.Now,
		challengeWindow: normalized.UNOChallengeWindow,
	}
	for seat, playerID := range playerIDs {
		engine.players[seat] = playerState{id: playerID}
	}

	// 初始化顺序固定为发牌、翻开局牌、应用开局效果。
	if err := engine.dealInitialHands(); err != nil {
		return nil, err
	}
	openingCard, err := engine.drawOpeningCard()
	if err != nil {
		return nil, err
	}
	engine.discardPile = append(engine.discardPile, openingCard)
	if err := engine.applyOpeningCard(openingCard); err != nil {
		return nil, err
	}
	return engine, nil
}

// validatePlayerIDs 校验人数、玩家 ID 正数约束及唯一性。
func validatePlayerIDs(playerIDs []int64) error {
	if len(playerIDs) < minPlayerCount || len(playerIDs) > maxPlayerCount {
		return fmt.Errorf("%w: player count must be 2-6", ErrInvalidPlayers)
	}
	seen := make(map[int64]struct{}, len(playerIDs))
	for _, playerID := range playerIDs {
		if playerID <= 0 {
			return fmt.Errorf("%w: player ID must be positive", ErrInvalidPlayers)
		}
		if _, exists := seen[playerID]; exists {
			return fmt.Errorf("%w: duplicate player %d", ErrInvalidPlayers, playerID)
		}
		seen[playerID] = struct{}{}
	}
	return nil
}

// dealInitialHands 连续七轮按座位顺序每人发一张牌。
func (e *Engine) dealInitialHands() error {
	for round := 0; round < InitialHandSize; round++ {
		for seat := range e.players {
			card, ok := e.popDrawPile()
			if !ok {
				return fmt.Errorf("deal initial hand: %w", ErrInvalidSnapshot)
			}
			e.players[seat].hand = append(e.players[seat].hand, card)
		}
	}
	return nil
}

// drawOpeningCard 拒绝将加四万能牌作为开局牌，并把它放回牌堆底部后重新洗牌。
// 先移动牌再洗牌可保证测试注入空操作洗牌器时循环仍能向前推进。
func (e *Engine) drawOpeningCard() (Card, error) {
	for attempts := 0; attempts < StandardDeckSize; attempts++ {
		card, ok := e.popDrawPile()
		if !ok {
			return Card{}, fmt.Errorf("draw opening card: %w", ErrInvalidSnapshot)
		}
		if card.Kind != KindWildDrawFour {
			return card, nil
		}

		// 抽牌堆顶位于切片末尾，插入切片开头即放回牌堆底部。
		returned := make([]Card, 0, len(e.drawPile)+1)
		returned = append(returned, card)
		returned = append(returned, e.drawPile...)
		e.drawPile = returned
		if err := e.shuffle(e.drawPile); err != nil {
			return Card{}, fmt.Errorf("reshuffle rejected opening card: %w", err)
		}
	}
	return Card{}, fmt.Errorf("draw opening card did not converge: %w", ErrInvalidSnapshot)
}

// applyOpeningCard 应用首张弃牌的特殊规则。
// 首位候选玩家固定为庄家顺时针方向的下一座位。
func (e *Engine) applyOpeningCard(card Card) error {
	firstSeat := e.stepFrom(e.dealerSeat, 1)
	e.currentColor = card.Color

	switch card.Kind {
	case KindNumber:
		e.currentSeat = firstSeat
	case KindSkip:
		e.currentSeat = e.stepFrom(firstSeat, 1)
	case KindReverse:
		if len(e.players) == 2 {
			// 双人局中反转等价于跳过首位候选玩家，因此改由庄家行动。
			e.currentSeat = e.stepFrom(firstSeat, 1)
			return nil
		}
		e.direction = DirectionCounterClockwise
		e.currentSeat = e.stepFrom(e.dealerSeat, 1)
	case KindDrawTwo:
		if _, err := e.drawCardsTo(firstSeat, 2); err != nil {
			return fmt.Errorf("opening draw two: %w", err)
		}
		e.currentSeat = e.stepFrom(firstSeat, 1)
	case KindWild:
		// 开局普通万能牌必须由首位玩家显式选色，但选色后不再推进座位。
		e.currentColor = ColorNone
		e.currentSeat = firstSeat
		e.phase = PhaseAwaitingColor
		e.pendingColor = &pendingColor{seat: firstSeat, card: card, opening: true}
	default:
		return fmt.Errorf("unsupported opening card %s: %w", card, ErrIllegalCard)
	}
	return nil
}

// popDrawPile 移除并返回抽牌堆顶；切片末尾表示牌堆顶部。
func (e *Engine) popDrawPile() (Card, bool) {
	if len(e.drawPile) == 0 {
		return Card{}, false
	}
	lastIndex := len(e.drawPile) - 1
	card := e.drawPile[lastIndex]
	e.drawPile = e.drawPile[:lastIndex]
	return card, true
}
