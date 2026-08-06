package uno

import (
	"fmt"
	"time"
)

// Play 从当前玩家手中移除一张合法实体牌。
// 万能牌会先进入 PhaseAwaitingColor，待 ChooseColor 完成后才结算牌面效果和回合流转。
func (e *Engine) Play(playerID int64, cardID CardID, sayUNO bool) error {
	// 所有校验均在修改状态前完成，失败命令不会留下部分变更。
	if e.phase == PhaseFinished {
		return ErrGameNotPlaying
	}
	if e.phase != PhasePlaying && e.phase != PhaseAwaitingDrawDecision {
		return fmt.Errorf("%w: phase %s does not accept play", ErrActionNotAllowed, e.phase)
	}
	seat, err := e.requireCurrentPlayer(playerID)
	if err != nil {
		return err
	}
	cardIndex, found := e.handCardIndex(seat, cardID)
	if !found {
		return ErrCardNotFound
	}
	card := e.players[seat].hand[cardIndex]
	if e.phase == PhaseAwaitingDrawDecision && card.ID != e.drawnCardID {
		return fmt.Errorf("%w: only the just-drawn card may be played", ErrIllegalCard)
	}
	if !e.cardPlayable(card) {
		return fmt.Errorf("%w: %s does not match current state", ErrIllegalCard, card)
	}

	card = e.removeHandCard(seat, cardIndex)
	e.drawnCardID = 0
	e.discardPile = append(e.discardPile, card)
	e.recordUNOAfterPlay(seat, sayUNO)
	// 空手不一定立即获胜：末牌若带罚牌效果，必须等罚牌链完全结算。
	if len(e.players[seat].hand) == 0 {
		e.addWinnerCandidate(seat)
	}

	if card.IsWild() {
		// 万能牌采用两阶段提交，避免房间层在出牌命令中隐式猜测颜色。
		e.currentColor = ColorNone
		e.phase = PhaseAwaitingColor
		e.pendingColor = &pendingColor{seat: seat, card: card}
		return nil
	}

	e.currentColor = card.Color
	e.applyCardEffect(seat, card)
	return nil
}

// ChooseColor 完成开局或玩家打出的万能牌选色，只有记录中的选色玩家可以调用。
func (e *Engine) ChooseColor(playerID int64, color Color) error {
	if e.phase == PhaseFinished {
		return ErrGameNotPlaying
	}
	if !color.Valid() {
		return ErrInvalidColor
	}
	if e.phase != PhaseAwaitingColor || e.pendingColor == nil {
		return fmt.Errorf("%w: no color choice is pending", ErrActionNotAllowed)
	}
	chooser := e.pendingColor
	if e.players[chooser.seat].id != playerID {
		if _, found := e.playerSeat(playerID); !found {
			return ErrPlayerNotFound
		}
		return ErrNotYourTurn
	}

	e.currentColor = color
	e.pendingColor = nil
	e.phase = PhasePlaying
	if chooser.opening {
		// 开局万能牌没有出牌者，也不产生额外的回合推进效果。
		return nil
	}
	e.applyCardEffect(chooser.seat, chooser.card)
	return nil
}

// Draw 在有累计罚牌时一次性接受全部罚牌并结束回合，否则正常摸一张牌。
// 普通摸牌若可打出，引擎会进入 PhaseAwaitingDrawDecision 等待玩家出牌或过牌。
func (e *Engine) Draw(playerID int64) (DrawResult, error) {
	if e.phase == PhaseFinished {
		return DrawResult{}, ErrGameNotPlaying
	}
	if e.phase != PhasePlaying {
		return DrawResult{}, fmt.Errorf("%w: phase %s does not accept draw", ErrActionNotAllowed, e.phase)
	}
	seat, err := e.requireCurrentPlayer(playerID)
	if err != nil {
		return DrawResult{}, err
	}

	if e.pendingDraw > 0 {
		// drawCardsTo 保证洗牌失败时回滚牌堆，因此此处可在成功后再提交回合状态。
		penalty := e.pendingDraw
		drawn, drawErr := e.drawCardsTo(seat, penalty)
		if drawErr != nil {
			return DrawResult{}, drawErr
		}
		e.pendingDraw = 0
		e.currentSeat = e.stepFrom(seat, 1)
		e.finishIfResolved()
		return DrawResult{
			Cards:     cloneCards(drawn),
			Penalty:   true,
			TurnEnded: true,
		}, nil
	}

	card, ok, drawErr := e.drawOne()
	if drawErr != nil {
		return DrawResult{}, drawErr
	}
	if !ok {
		// 极端情况下只剩弃牌堆顶且无牌可回收，视为摸空并直接结束回合。
		e.currentSeat = e.stepFrom(seat, 1)
		return DrawResult{TurnEnded: true}, nil
	}
	e.players[seat].hand = append(e.players[seat].hand, card)
	e.clearUNOChallengeFor(playerID)

	canPlay := e.cardPlayable(card)
	if canPlay {
		// 决策阶段只授权这张实体牌，不能借机打出原手牌中的同牌面牌。
		e.phase = PhaseAwaitingDrawDecision
		e.drawnCardID = card.ID
		return DrawResult{Cards: []Card{card}, CanPlay: true}, nil
	}
	e.currentSeat = e.stepFrom(seat, 1)
	return DrawResult{Cards: []Card{card}, TurnEnded: true}, nil
}

// Pass 保留刚摸到且可打出的牌，并结束当前玩家回合。
func (e *Engine) Pass(playerID int64) error {
	if e.phase == PhaseFinished {
		return ErrGameNotPlaying
	}
	if e.phase != PhaseAwaitingDrawDecision || e.drawnCardID == 0 {
		return fmt.Errorf("%w: no drawn-card decision is pending", ErrActionNotAllowed)
	}
	seat, err := e.requireCurrentPlayer(playerID)
	if err != nil {
		return err
	}
	e.drawnCardID = 0
	e.phase = PhasePlaying
	e.currentSeat = e.stepFrom(seat, 1)
	return nil
}

// PlayableCards 返回当前动作阶段可出的实体牌副本。
// 在摸牌决策阶段，结果至多包含刚摸到的那一张牌。
func (e *Engine) PlayableCards(playerID int64) ([]Card, error) {
	if e.phase == PhaseFinished {
		return nil, ErrGameNotPlaying
	}
	if e.phase != PhasePlaying && e.phase != PhaseAwaitingDrawDecision {
		return nil, fmt.Errorf("%w: phase %s has no playable cards", ErrActionNotAllowed, e.phase)
	}
	seat, err := e.requireCurrentPlayer(playerID)
	if err != nil {
		return nil, err
	}
	playable := make([]Card, 0, len(e.players[seat].hand))
	for _, card := range e.players[seat].hand {
		if e.phase == PhaseAwaitingDrawDecision && card.ID != e.drawnCardID {
			continue
		}
		if e.cardPlayable(card) {
			playable = append(playable, card)
		}
	}
	return playable, nil
}

// CallUNO 让玩家在被他人抓罚前主动补喊 UNO，并清除自己的挑战状态。
func (e *Engine) CallUNO(playerID int64) error {
	if e.phase == PhaseFinished {
		return ErrGameNotPlaying
	}
	if _, found := e.playerSeat(playerID); !found {
		return ErrPlayerNotFound
	}
	challenge, active := e.activeUNOChallenge(playerID)
	if !active {
		return ErrNoUNOChallenge
	}
	delete(e.unoChallenges, challenge.PlayerID)
	return nil
}

// CatchUNO 让漏喊 UNO 的目标玩家摸两张罚牌。
// 该操作不受当前回合限制，挑战窗口内任意其他在座玩家均可调用。
func (e *Engine) CatchUNO(challengerID, targetID int64) (DrawResult, error) {
	if e.phase == PhaseFinished {
		return DrawResult{}, ErrGameNotPlaying
	}
	if _, found := e.playerSeat(challengerID); !found {
		return DrawResult{}, ErrPlayerNotFound
	}
	targetSeat, found := e.playerSeat(targetID)
	if !found {
		return DrawResult{}, ErrPlayerNotFound
	}
	if challengerID == targetID {
		return DrawResult{}, ErrCannotCatchSelf
	}
	_, active := e.activeUNOChallenge(targetID)
	if !active {
		return DrawResult{}, ErrNoUNOChallenge
	}
	drawn, err := e.drawCardsTo(targetSeat, 2)
	if err != nil {
		return DrawResult{}, err
	}
	delete(e.unoChallenges, targetID)
	return DrawResult{Cards: cloneCards(drawn), Penalty: true}, nil
}

// ExpireUNOChallenges 清除已经超时的抓罚窗口，并返回受影响的玩家 ID。
// 房间定时器可以调用本方法，再向客户端发布最新视图。
func (e *Engine) ExpireUNOChallenges() []int64 {
	now := e.now().UTC()
	expired := make([]int64, 0)
	// 按座位顺序扫描，使返回顺序稳定，便于事件广播和测试。
	for seat := range e.players {
		playerID := e.players[seat].id
		challenge, found := e.unoChallenges[playerID]
		if !found {
			continue
		}
		if deadlineReached(now, challenge.ExpiresAt) {
			delete(e.unoChallenges, playerID)
			expired = append(expired, playerID)
		}
	}
	return expired
}

// recordUNOAfterPlay 仅更新本次出牌玩家的 UNO 抓罚状态。
func (e *Engine) recordUNOAfterPlay(seat int, sayUNO bool) {
	playerID := e.players[seat].id
	if len(e.players[seat].hand) != 1 || sayUNO {
		delete(e.unoChallenges, playerID)
		return
	}
	e.unoChallenges[playerID] = UNOChallenge{
		PlayerID:  playerID,
		ExpiresAt: e.now().UTC().Add(e.challengeWindow),
	}
}

// activeUNOChallenge 返回目标玩家仍有效的挑战，并顺手清理已过期记录。
func (e *Engine) activeUNOChallenge(playerID int64) (UNOChallenge, bool) {
	challenge, found := e.unoChallenges[playerID]
	if !found {
		return UNOChallenge{}, false
	}
	if deadlineReached(e.now().UTC(), challenge.ExpiresAt) {
		delete(e.unoChallenges, playerID)
		return UNOChallenge{}, false
	}
	return challenge, true
}

// deadlineReached 将恰好到达截止时间也视为已过期。
func deadlineReached(now, deadline time.Time) bool {
	return !now.Before(deadline)
}
