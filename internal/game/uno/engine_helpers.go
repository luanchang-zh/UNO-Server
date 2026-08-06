package uno

import "fmt"

// playerSeat 根据玩家 ID 查找其座位下标。
func (e *Engine) playerSeat(playerID int64) (int, bool) {
	for seat := range e.players {
		if e.players[seat].id == playerID {
			return seat, true
		}
	}
	return 0, false
}

// currentPlayerID 返回当前行动玩家 ID；当前没有行动者时返回零。
func (e *Engine) currentPlayerID() int64 {
	if e.currentSeat < 0 || e.currentSeat >= len(e.players) {
		return 0
	}
	return e.players[e.currentSeat].id
}

// stepFrom 从指定座位沿当前方向前进正数个座位，并处理首尾环绕。
func (e *Engine) stepFrom(seat, steps int) int {
	playerCount := len(e.players)
	position := seat + steps*int(e.direction)
	// Go 的负数取模结果仍为负数，因此逆时针越界后需要再补一个玩家总数。
	position %= playerCount
	if position < 0 {
		position += playerCount
	}
	return position
}

// requireCurrentPlayer 同时校验玩家是否在座，以及是否拥有当前回合。
func (e *Engine) requireCurrentPlayer(playerID int64) (int, error) {
	seat, found := e.playerSeat(playerID)
	if !found {
		return 0, ErrPlayerNotFound
	}
	if seat != e.currentSeat {
		return 0, ErrNotYourTurn
	}
	return seat, nil
}

// handCardIndex 按实体牌 ID 在指定玩家手牌中查找下标。
func (e *Engine) handCardIndex(seat int, cardID CardID) (int, bool) {
	for index := range e.players[seat].hand {
		if e.players[seat].hand[index].ID == cardID {
			return index, true
		}
	}
	return 0, false
}

// removeHandCard 移除并返回一张实体牌，同时保持其余手牌顺序稳定，避免客户端列表抖动。
func (e *Engine) removeHandCard(seat, index int) Card {
	hand := e.players[seat].hand
	card := hand[index]
	copy(hand[index:], hand[index+1:])
	e.players[seat].hand = hand[:len(hand)-1]
	return card
}

// topCard 返回当前弃牌堆顶；引擎和快照校验共同保证弃牌堆非空。
func (e *Engine) topCard() Card {
	return e.discardPile[len(e.discardPile)-1]
}

// cardPlayable 在累计罚牌期间只采用叠罚规则，否则采用普通牌面匹配规则。
func (e *Engine) cardPlayable(card Card) bool {
	if e.pendingDraw > 0 {
		return card.IsDrawPenalty()
	}
	return card.Matches(e.topCard(), e.currentColor)
}

// drawOne 摸取一张牌；抽牌堆为空时先回收除堆顶外的弃牌。
func (e *Engine) drawOne() (Card, bool, error) {
	if len(e.drawPile) == 0 {
		if err := e.recycleDiscardPile(); err != nil {
			return Card{}, false, err
		}
	}
	card, ok := e.popDrawPile()
	return card, ok, nil
}

// recycleDiscardPile 保留弃牌堆顶，并将其余弃牌洗成新的抽牌堆。
// 弃牌堆只有堆顶一张时没有可回收的牌。
func (e *Engine) recycleDiscardPile() error {
	if len(e.discardPile) <= 1 {
		return nil
	}
	topIndex := len(e.discardPile) - 1
	recycled := cloneCards(e.discardPile[:topIndex])
	top := e.discardPile[topIndex]
	// 先在副本上洗牌，确保洗牌器失败时两个原牌堆均保持不变。
	if err := e.shuffle(recycled); err != nil {
		return fmt.Errorf("recycle discard pile: %w", err)
	}
	e.discardPile = []Card{top}
	e.drawPile = recycled
	return nil
}

// drawCardsTo 最多向指定座位发 count 张牌。
// 极端状态下全部可用牌不足时允许少发，但洗牌失败会完整回滚牌堆。
func (e *Engine) drawCardsTo(seat, count int) ([]Card, error) {
	// 保存命令前快照，使一次多张摸牌具备原子性。
	originalDrawPile := cloneCards(e.drawPile)
	originalDiscardPile := cloneCards(e.discardPile)
	drawn := make([]Card, 0, count)
	for len(drawn) < count {
		card, ok, err := e.drawOne()
		if err != nil {
			e.drawPile = originalDrawPile
			e.discardPile = originalDiscardPile
			return nil, err
		}
		if !ok {
			break
		}
		drawn = append(drawn, card)
	}
	e.players[seat].hand = append(e.players[seat].hand, drawn...)
	if len(drawn) > 0 {
		// 原本空手的候选胜者一旦重新拿到牌，就不能继续参与本轮胜者判定。
		e.removeWinnerCandidate(seat)
	}
	if len(e.players[seat].hand) != 1 {
		e.clearUNOChallengeFor(e.players[seat].id)
	}
	return drawn, nil
}

// applyCardEffect 在牌面信息完整后更新方向或累计罚牌，并推进当前座位。
func (e *Engine) applyCardEffect(seat int, card Card) {
	e.phase = PhasePlaying
	switch card.Kind {
	case KindSkip:
		e.currentSeat = e.stepFrom(seat, 2)
	case KindReverse:
		if len(e.players) == 2 {
			// 双人局反转等价于跳过另一名玩家，出牌者继续行动。
			e.currentSeat = e.stepFrom(seat, 2)
			break
		}
		e.direction = -e.direction
		e.currentSeat = e.stepFrom(seat, 1)
	case KindDrawTwo:
		e.pendingDraw += 2
		e.currentSeat = e.stepFrom(seat, 1)
	case KindWildDrawFour:
		e.pendingDraw += 4
		e.currentSeat = e.stepFrom(seat, 1)
	default:
		e.currentSeat = e.stepFrom(seat, 1)
	}
	e.finishIfResolved()
}

// finishIfResolved 仅在末牌的全部罚牌已经实际摸取后结算空手胜者，
// 从而保证罚摸到的牌会计入最终得分。
func (e *Engine) finishIfResolved() {
	if e.pendingDraw > 0 || e.phase == PhaseAwaitingColor {
		return
	}
	active := e.winnerCandidates[:0]
	// 叠罚期间候选者也可能被迫摸牌，因此结算前必须再次筛除非空手玩家。
	for _, seat := range e.winnerCandidates {
		if len(e.players[seat].hand) == 0 {
			active = append(active, seat)
		}
	}
	e.winnerCandidates = active
	if len(e.winnerCandidates) > 0 {
		e.finishRound(e.winnerCandidates[0])
	}
}

// finishRound 计算不可变的逐玩家结算结果，并将引擎切换为终态。
func (e *Engine) finishRound(winnerSeat int) {
	winnerID := e.players[winnerSeat].id
	results := make([]PlayerResult, 0, len(e.players))
	winnerScore := 0
	for seat := range e.players {
		handPoints := 0
		for _, card := range e.players[seat].hand {
			handPoints += card.Points()
		}
		isWinner := seat == winnerSeat
		if !isWinner {
			winnerScore += handPoints
		}
		results = append(results, PlayerResult{
			PlayerID:   e.players[seat].id,
			IsWinner:   isWinner,
			HandPoints: handPoints,
			CardsLeft:  len(e.players[seat].hand),
		})
	}
	for index := range results {
		// UNO 单局计分只归胜者，其他玩家的 Score 保持为零。
		if results[index].IsWinner {
			results[index].Score = winnerScore
		}
	}

	// 终态主动清除所有临时动作，防止恢复或视图层误判仍有命令待处理。
	e.result = &RoundResult{WinnerID: winnerID, Score: winnerScore, Players: results}
	e.winnerCandidates = []int{winnerSeat}
	e.phase = PhaseFinished
	e.currentSeat = -1
	e.pendingDraw = 0
	e.drawnCardID = 0
	e.pendingColor = nil
	e.unoChallenges = nil
}

// addWinnerCandidate 按玩家清空手牌的先后顺序记录候选胜者，且不会重复插入。
func (e *Engine) addWinnerCandidate(seat int) {
	for _, candidate := range e.winnerCandidates {
		if candidate == seat {
			return
		}
	}
	e.winnerCandidates = append(e.winnerCandidates, seat)
}

// removeWinnerCandidate 移除在末牌效果完成前又被迫摸牌的候选胜者。
func (e *Engine) removeWinnerCandidate(seat int) {
	for index, candidate := range e.winnerCandidates {
		if candidate != seat {
			continue
		}
		copy(e.winnerCandidates[index:], e.winnerCandidates[index+1:])
		e.winnerCandidates = e.winnerCandidates[:len(e.winnerCandidates)-1]
		return
	}
}

// clearUNOChallengeFor 清除指定玩家的漏喊 UNO 抓罚状态。
func (e *Engine) clearUNOChallengeFor(playerID int64) {
	delete(e.unoChallenges, playerID)
}
