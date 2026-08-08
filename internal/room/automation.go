package room

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
)

// scheduleTurnTimer 根据当前行动玩家状态安排手动超时或托管行动事件。
func (r *Room) scheduleTurnTimer() {
	r.cancelTurnTimer()
	if r.Phase != PhasePlaying || r.engine == nil {
		return
	}
	_, member, found := r.currentActorView()
	if !found {
		return
	}

	delay := r.turnTimeout
	kind := commandTurnTimeout
	if member.AutoPlay || !member.Connected {
		delay = r.managedActionDelay
		kind = commandAutoPlay
	}
	token := r.turnToken
	r.turnDeadline = time.Now().UTC().Add(delay)
	r.turnTimer = time.AfterFunc(delay, func() {
		command := roomCommand{kind: kind, timerToken: token}
		select {
		case <-r.closed:
		case r.mailbox <- command:
		}
	})
}

// cancelTurnTimer 停止当前计时器并递增令牌，使已经入队的旧事件失效。
func (r *Room) cancelTurnTimer() {
	if r.turnTimer != nil {
		r.turnTimer.Stop()
		r.turnTimer = nil
	}
	r.turnDeadline = time.Time{}
	r.turnToken++
}

// handleAutomatedTurn 在 mailbox 内执行一次超时或托管行动。
func (r *Room) handleAutomatedTurn(kind commandKind, token uint64) {
	if token != r.turnToken || r.Phase != PhasePlaying || r.engine == nil {
		return
	}
	view, member, found := r.currentActorView()
	if !found {
		return
	}
	memberStateChanged := false
	if kind == commandTurnTimeout {
		member.TimeoutStrikes++
		if member.TimeoutStrikes >= r.timeoutStrikeLimit {
			member.AutoPlay = true
		}
		memberStateChanged = true
	}

	if err := r.performAutomaticAction(member.PlayerID, view); err != nil {
		if memberStateChanged {
			r.broadcastState()
		}
		r.scheduleTurnTimer()
		return
	}
	r.finishAutomatedAction(memberStateChanged)
}

// performAutomaticAction 按当前引擎阶段选择一条确定合法的托管动作。
func (r *Room) performAutomaticAction(playerID int64, view uno.View) error {
	switch view.Phase {
	case uno.PhaseAwaitingColor:
		return r.engine.ChooseColor(playerID, randomColor())
	case uno.PhaseAwaitingDrawDecision:
		if len(view.PlayableCardIDs) == 0 {
			return r.engine.Pass(playerID)
		}
		return r.playAutomaticCard(playerID, randomCardID(view.PlayableCardIDs))
	case uno.PhasePlaying:
		// 叠罚响应超时按需求固定承受，不自动选择继续叠加。
		if view.PendingDraw > 0 {
			_, err := r.engine.Draw(playerID)
			return err
		}
		if len(view.PlayableCardIDs) > 0 {
			return r.playAutomaticCard(playerID, randomCardID(view.PlayableCardIDs))
		}
		drawResult, err := r.engine.Draw(playerID)
		if err != nil || !drawResult.CanPlay || len(drawResult.Cards) == 0 {
			return err
		}
		return r.playAutomaticCard(playerID, drawResult.Cards[0].ID)
	default:
		return nil
	}
}

// playAutomaticCard 打出托管选择的牌，并在需要时立即完成万能牌选色。
func (r *Room) playAutomaticCard(playerID int64, cardID uno.CardID) error {
	if err := r.engine.Play(playerID, cardID, true); err != nil {
		return err
	}
	view, err := r.engine.ViewFor(playerID)
	if err != nil || view.Phase != uno.PhaseAwaitingColor {
		return err
	}
	return r.engine.ChooseColor(playerID, randomColor())
}

// finishAutomatedAction 广播自动动作结果，并为新的行动玩家安排下一事件。
func (r *Room) finishAutomatedAction(memberStateChanged bool) {
	view, _, found := r.currentActorView()
	if !found && r.engine != nil && len(r.Members) > 0 {
		view, _ = r.engine.ViewFor(r.Members[0].PlayerID)
	}
	shouldPersistSettlement := view.Phase == uno.PhaseFinished
	if shouldPersistSettlement {
		r.Phase = PhaseSettled
		r.cancelTurnTimer()
		memberStateChanged = true
	}
	if memberStateChanged {
		r.broadcastState()
	}
	if r.Phase == PhasePlaying {
		r.scheduleTurnTimer()
	}
	r.broadcastGameState()
	if shouldPersistSettlement {
		if err := r.persistSettlement(view.Result); err != nil {
			r.logger.WithContext(context.Background()).Error(
				"自动牌局结算持久化失败",
				"room_id", r.ID,
				"match_id", r.matchID,
				"error", err,
			)
		}
	}
}

// currentActorView 返回当前行动玩家自己的安全视图和成员状态。
func (r *Room) currentActorView() (uno.View, *Member, bool) {
	if r.engine == nil || len(r.Members) == 0 {
		return uno.View{}, nil, false
	}
	publicView, err := r.engine.ViewFor(r.Members[0].PlayerID)
	if err != nil || publicView.Phase == uno.PhaseFinished {
		return publicView, nil, false
	}
	actorID := publicView.CurrentPlayerID
	if publicView.Phase == uno.PhaseAwaitingColor {
		actorID = publicView.ColorChooserID
	}
	member := r.findMember(actorID)
	if member == nil {
		return uno.View{}, nil, false
	}
	actorView, err := r.engine.ViewFor(actorID)
	if err != nil {
		return uno.View{}, nil, false
	}
	return actorView, member, true
}

// isCurrentGamePlayer 判断指定玩家是否拥有当前阶段的行动权。
func (r *Room) isCurrentGamePlayer(playerID int64) bool {
	_, member, found := r.currentActorView()
	return found && member.PlayerID == playerID
}

// randomCardID 从服务端已经校验过的合法实体牌中等概率选择一张。
func randomCardID(cardIDs []uno.CardID) uno.CardID {
	return cardIDs[secureRandomIndex(len(cardIDs))]
}

// randomColor 从四种合法颜色中等概率选择一种。
func randomColor() uno.Color {
	colors := uno.Colors()
	return colors[secureRandomIndex(len(colors))]
}

// secureRandomIndex 返回 [0, length) 内的安全随机下标；随机源失败时回退到零。
func secureRandomIndex(length int) int {
	if length <= 1 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(length)))
	if err != nil {
		return 0
	}
	return int(value.Int64())
}
