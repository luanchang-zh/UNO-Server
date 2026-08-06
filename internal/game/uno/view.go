package uno

// PlayerView 只包含单个座位可向全房间公开的信息。
type PlayerView struct {
	// PlayerID 是该座位绑定的玩家 ID。
	PlayerID int64 `json:"player_id"`
	// Seat 是玩家在本局中的稳定座位下标。
	Seat int `json:"seat"`
	// Cards 是玩家当前手牌数量，不暴露具体牌面。
	Cards int `json:"cards"`
}

// View 是面向单名玩家的安全投影视图。
// 其他玩家只暴露手牌数量，Hand 与 DrawnCardID 仅属于请求视图的玩家。
type View struct {
	// Phase 是当前状态机阶段。
	Phase Phase `json:"phase"`
	// Players 按座位顺序提供所有玩家的公开信息。
	Players []PlayerView `json:"players"`
	// Hand 是请求玩家自己的完整手牌副本。
	Hand []Card `json:"hand"`
	// PlayableCardIDs 是请求玩家在当前阶段可出的实体牌 ID。
	PlayableCardIDs []CardID `json:"playable_card_ids,omitempty"`
	// TopCard 是当前弃牌堆顶。
	TopCard Card `json:"top_card"`
	// CurrentColor 是当前实际生效颜色。
	CurrentColor Color `json:"current_color,omitempty"`
	// Direction 是当前座位轮转方向。
	Direction Direction `json:"direction"`
	// CurrentPlayerID 是当前行动玩家；终局时为零。
	CurrentPlayerID int64 `json:"current_player_id,omitempty"`
	// ColorChooserID 是当前唯一有权完成万能牌选色的玩家。
	ColorChooserID int64 `json:"color_chooser_id,omitempty"`
	// PendingDraw 是当前累计且尚未接受的罚牌数量。
	PendingDraw int `json:"pending_draw"`
	// DrawPileCount 是抽牌堆剩余张数。
	DrawPileCount int `json:"draw_pile_count"`
	// DiscardPileCount 是弃牌堆当前张数。
	DiscardPileCount int `json:"discard_pile_count"`
	// DrawnCardID 只向当前摸牌决策者暴露刚摸到的实体牌 ID。
	DrawnCardID CardID `json:"drawn_card_id,omitempty"`
	// UNOChallenges 是仍在有效窗口内的漏喊 UNO 记录。
	UNOChallenges []UNOChallenge `json:"uno_challenges,omitempty"`
	// WinnerCandidateIDs 是等待末牌效果完成后结算的候选胜者 ID。
	WinnerCandidateIDs []int64 `json:"winner_candidate_ids,omitempty"`
	// Result 是终局结算的深拷贝，进行中为 nil。
	Result *RoundResult `json:"result,omitempty"`
}

// ViewFor 为一名在座玩家创建与引擎分离的公私信息投影视图。
func (e *Engine) ViewFor(playerID int64) (View, error) {
	requesterSeat, found := e.playerSeat(playerID)
	if !found {
		return View{}, ErrPlayerNotFound
	}

	// 其他座位只投影公开计数，不复制其私有手牌。
	players := make([]PlayerView, 0, len(e.players))
	for seat := range e.players {
		players = append(players, PlayerView{
			PlayerID: e.players[seat].id,
			Seat:     seat,
			Cards:    len(e.players[seat].hand),
		})
	}
	view := View{
		Phase:            e.phase,
		Players:          players,
		Hand:             cloneCards(e.players[requesterSeat].hand),
		TopCard:          e.topCard(),
		CurrentColor:     e.currentColor,
		Direction:        e.direction,
		CurrentPlayerID:  e.currentPlayerID(),
		PendingDraw:      e.pendingDraw,
		DrawPileCount:    len(e.drawPile),
		DiscardPileCount: len(e.discardPile),
		Result:           cloneRoundResult(e.result),
	}

	if e.pendingColor != nil {
		view.ColorChooserID = e.players[e.pendingColor.seat].id
	}
	for _, seat := range e.winnerCandidates {
		view.WinnerCandidateIDs = append(view.WinnerCandidateIDs, e.players[seat].id)
	}
	if requesterSeat == e.currentSeat && e.phase == PhaseAwaitingDrawDecision {
		// 该实体牌 ID 属于当前玩家的私有决策信息。
		view.DrawnCardID = e.drawnCardID
	}
	if requesterSeat == e.currentSeat &&
		(e.phase == PhasePlaying || e.phase == PhaseAwaitingDrawDecision) {
		for _, card := range e.players[requesterSeat].hand {
			if e.phase == PhaseAwaitingDrawDecision && card.ID != e.drawnCardID {
				continue
			}
			if e.cardPlayable(card) {
				view.PlayableCardIDs = append(view.PlayableCardIDs, card.ID)
			}
		}
	}

	// 视图层过滤超时记录但不修改引擎，状态清理由显式过期命令负责。
	now := e.now().UTC()
	for seat := range e.players {
		challenge, active := e.unoChallenges[e.players[seat].id]
		if active && !deadlineReached(now, challenge.ExpiresAt) {
			view.UNOChallenges = append(view.UNOChallenges, challenge)
		}
	}
	return view, nil
}

// cloneRoundResult 返回可选结算对象的深拷贝。
func cloneRoundResult(result *RoundResult) *RoundResult {
	if result == nil {
		return nil
	}
	cloned := *result
	cloned.Players = append([]PlayerResult(nil), result.Players...)
	return &cloned
}
