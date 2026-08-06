package uno

import "fmt"

// SnapshotSchemaVersion 是当前引擎状态持久化结构的版本号。
const SnapshotSchemaVersion = currentSnapshotVersion

// PlayerSnapshot 保存单名玩家仅用于持久化的权威私有状态。
type PlayerSnapshot struct {
	// PlayerID 是该座位绑定的玩家 ID。
	PlayerID int64 `json:"player_id"`
	// Hand 是该玩家完整的实体手牌。
	Hand []Card `json:"hand"`
}

// PendingColorSnapshot 持久化尚未完成的万能牌选色动作。
type PendingColorSnapshot struct {
	// PlayerID 是唯一有权完成选色的玩家 ID。
	PlayerID int64 `json:"player_id"`
	// Card 是已经进入弃牌堆、等待选色的万能牌。
	Card Card `json:"card"`
	// Opening 表示该动作是否由开局翻牌触发。
	Opening bool `json:"opening"`
}

// Snapshot 是用于 Redis 持久化或崩溃恢复的完整深拷贝状态。
// 快照包含所有玩家手牌，绝不能直接广播给客户端。
type Snapshot struct {
	// SchemaVersion 用于拒绝不兼容的持久化结构。
	SchemaVersion int `json:"schema_version"`
	// Players 按座位顺序保存所有玩家及其私有手牌。
	Players []PlayerSnapshot `json:"players"`
	// DrawPile 按切片末尾为堆顶的顺序保存抽牌堆。
	DrawPile []Card `json:"draw_pile"`
	// DiscardPile 按切片末尾为堆顶的顺序保存弃牌堆。
	DiscardPile []Card `json:"discard_pile"`
	// DealerSeat 是本局庄家的座位下标。
	DealerSeat int `json:"dealer_seat"`
	// CurrentSeat 是当前行动座位；已结束时固定为 -1。
	CurrentSeat int `json:"current_seat"`
	// Direction 是当前座位轮转方向。
	Direction Direction `json:"direction"`
	// CurrentColor 是当前生效颜色，等待万能牌选色时为 ColorNone。
	CurrentColor Color `json:"current_color,omitempty"`
	// PendingDraw 是当前累计且尚未接受的罚牌数量。
	PendingDraw int `json:"pending_draw"`
	// Phase 是当前状态机阶段。
	Phase Phase `json:"phase"`
	// DrawnCardID 是摸牌决策阶段唯一允许立即打出的实体牌 ID。
	DrawnCardID CardID `json:"drawn_card_id,omitempty"`
	// PendingColor 保存尚未完成的万能牌选色动作。
	PendingColor *PendingColorSnapshot `json:"pending_color,omitempty"`
	// UNOChallenges 按座位顺序保存仍可被抓罚的漏喊记录。
	UNOChallenges []UNOChallenge `json:"uno_challenges,omitempty"`
	// WinnerCandidates 按清空手牌先后保存等待末牌效果结算的座位。
	WinnerCandidates []int `json:"winner_candidates,omitempty"`
	// Result 仅在 PhaseFinished 阶段保存最终结算。
	Result *RoundResult `json:"result,omitempty"`
}

// Snapshot 返回与运行中引擎完全分离的完整状态副本。
func (e *Engine) Snapshot() Snapshot {
	players := make([]PlayerSnapshot, 0, len(e.players))
	for seat := range e.players {
		players = append(players, PlayerSnapshot{
			PlayerID: e.players[seat].id,
			Hand:     cloneCards(e.players[seat].hand),
		})
	}
	snapshot := Snapshot{
		SchemaVersion:    SnapshotSchemaVersion,
		Players:          players,
		DrawPile:         cloneCards(e.drawPile),
		DiscardPile:      cloneCards(e.discardPile),
		DealerSeat:       e.dealerSeat,
		CurrentSeat:      e.currentSeat,
		Direction:        e.direction,
		CurrentColor:     e.currentColor,
		PendingDraw:      e.pendingDraw,
		Phase:            e.phase,
		DrawnCardID:      e.drawnCardID,
		WinnerCandidates: append([]int(nil), e.winnerCandidates...),
		Result:           cloneRoundResult(e.result),
	}
	if e.pendingColor != nil {
		// 对外持久化玩家 ID，而不是可能随房间重建变化的内部指针或引用。
		snapshot.PendingColor = &PendingColorSnapshot{
			PlayerID: e.players[e.pendingColor.seat].id,
			Card:     e.pendingColor.card,
			Opening:  e.pendingColor.opening,
		}
	}
	// 按座位顺序输出映射内容，保证序列化结果稳定。
	for seat := range e.players {
		if challenge, found := e.unoChallenges[e.players[seat].id]; found {
			snapshot.UNOChallenges = append(snapshot.UNOChallenges, challenge)
		}
	}
	return snapshot
}

// Restore 校验持久化状态并重建引擎；洗牌器和时钟等运行时依赖由 config 重新注入。
func Restore(snapshot Snapshot, config Config) (*Engine, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	normalized, err := normalizedConfig(config, len(snapshot.Players))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}

	// 所有切片和结算对象都重新复制，恢复后的引擎不会引用调用方内存。
	engine := &Engine{
		players:          make([]playerState, len(snapshot.Players)),
		drawPile:         cloneCards(snapshot.DrawPile),
		discardPile:      cloneCards(snapshot.DiscardPile),
		dealerSeat:       snapshot.DealerSeat,
		currentSeat:      snapshot.CurrentSeat,
		direction:        snapshot.Direction,
		currentColor:     snapshot.CurrentColor,
		pendingDraw:      snapshot.PendingDraw,
		phase:            snapshot.Phase,
		drawnCardID:      snapshot.DrawnCardID,
		winnerCandidates: append([]int(nil), snapshot.WinnerCandidates...),
		result:           cloneRoundResult(snapshot.Result),
		unoChallenges:    make(map[int64]UNOChallenge, len(snapshot.UNOChallenges)),
		shuffle:          normalized.Shuffle,
		now:              normalized.Now,
		challengeWindow:  normalized.UNOChallengeWindow,
	}
	for seat, player := range snapshot.Players {
		engine.players[seat] = playerState{id: player.PlayerID, hand: cloneCards(player.Hand)}
	}
	if snapshot.PendingColor != nil {
		seat, _ := engine.playerSeat(snapshot.PendingColor.PlayerID)
		engine.pendingColor = &pendingColor{
			seat:    seat,
			card:    snapshot.PendingColor.Card,
			opening: snapshot.PendingColor.Opening,
		}
	}
	for _, challenge := range snapshot.UNOChallenges {
		engine.unoChallenges[challenge.PlayerID] = challenge
	}
	return engine, nil
}

// validateSnapshot 拒绝可能导致引擎越界、状态机矛盾或破坏牌守恒的状态。
func validateSnapshot(snapshot Snapshot) error {
	// 从廉价结构校验逐步深入到牌库与结算校验，尽早返回最直接的诊断。
	if err := validateSnapshotShape(snapshot); err != nil {
		return err
	}
	if err := validateSnapshotLifecycle(snapshot); err != nil {
		return err
	}
	if err := validateSnapshotCards(snapshot); err != nil {
		return err
	}
	if err := validateSnapshotPendingActions(snapshot); err != nil {
		return err
	}
	if snapshot.Result != nil {
		if err := validateRoundResult(snapshot); err != nil {
			return invalidSnapshot("result: %v", err)
		}
	}
	return nil
}

// invalidSnapshot 构造携带诊断上下文、且可由 errors.Is 识别的快照错误。
func invalidSnapshot(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidSnapshot, fmt.Sprintf(format, args...))
}

// validateSnapshotShape 校验基础枚举、座位下标以及必需集合。
func validateSnapshotShape(snapshot Snapshot) error {
	if snapshot.SchemaVersion != SnapshotSchemaVersion {
		return invalidSnapshot("schema version %d", snapshot.SchemaVersion)
	}
	playerIDs := make([]int64, 0, len(snapshot.Players))
	for _, player := range snapshot.Players {
		playerIDs = append(playerIDs, player.PlayerID)
	}
	if err := validatePlayerIDs(playerIDs); err != nil {
		return invalidSnapshot("players: %v", err)
	}
	if snapshot.DealerSeat < 0 || snapshot.DealerSeat >= len(snapshot.Players) {
		return invalidSnapshot("dealer seat %d", snapshot.DealerSeat)
	}
	if snapshot.Direction != DirectionClockwise && snapshot.Direction != DirectionCounterClockwise {
		return invalidSnapshot("direction %d", snapshot.Direction)
	}
	if len(snapshot.DiscardPile) == 0 {
		return invalidSnapshot("discard pile is empty")
	}
	if snapshot.PendingDraw < 0 || !validPhase(snapshot.Phase) {
		return invalidSnapshot("phase %q or pending draw %d", snapshot.Phase, snapshot.PendingDraw)
	}
	seenCandidates := make(map[int]struct{}, len(snapshot.WinnerCandidates))
	// 候选胜者必须仍为空手，且其顺序本身参与最终胜者判定。
	for _, seat := range snapshot.WinnerCandidates {
		if seat < 0 || seat >= len(snapshot.Players) || len(snapshot.Players[seat].Hand) != 0 {
			return invalidSnapshot("invalid winner candidate seat %d", seat)
		}
		if _, duplicate := seenCandidates[seat]; duplicate {
			return invalidSnapshot("duplicate winner candidate seat %d", seat)
		}
		seenCandidates[seat] = struct{}{}
	}
	return nil
}

// validateSnapshotLifecycle 校验各阶段专属字段与当前颜色不变量。
func validateSnapshotLifecycle(snapshot Snapshot) error {
	if snapshot.Phase == PhaseFinished {
		if snapshot.CurrentSeat != -1 || snapshot.Result == nil ||
			snapshot.PendingDraw != 0 || len(snapshot.WinnerCandidates) == 0 {
			return invalidSnapshot("finished state is incomplete")
		}
	} else if snapshot.CurrentSeat < 0 || snapshot.CurrentSeat >= len(snapshot.Players) {
		return invalidSnapshot("current seat %d", snapshot.CurrentSeat)
	} else if snapshot.Result != nil {
		return invalidSnapshot("active state has a result")
	}
	if snapshot.Phase != PhaseFinished && len(snapshot.WinnerCandidates) > 0 &&
		snapshot.PendingDraw == 0 && snapshot.Phase != PhaseAwaitingColor {
		return invalidSnapshot("resolved winner candidate left in active state")
	}

	if snapshot.Phase == PhaseAwaitingColor {
		if snapshot.PendingColor == nil || snapshot.CurrentColor != ColorNone {
			return invalidSnapshot("awaiting color state is incomplete")
		}
	} else if snapshot.PendingColor != nil {
		return invalidSnapshot("pending color outside awaiting-color phase")
	}
	if snapshot.Phase == PhaseAwaitingDrawDecision {
		if snapshot.DrawnCardID == 0 || snapshot.PendingDraw != 0 {
			return invalidSnapshot("draw decision state is incomplete")
		}
	} else if snapshot.DrawnCardID != 0 {
		return invalidSnapshot("drawn card outside decision phase")
	}
	if snapshot.Phase != PhaseAwaitingColor && !snapshot.CurrentColor.Valid() {
		return invalidSnapshot("current color %q", snapshot.CurrentColor)
	}
	top := snapshot.DiscardPile[len(snapshot.DiscardPile)-1]
	// 非万能牌的有效颜色只能来自弃牌堆顶，防止快照伪造额外选色结果。
	if !top.IsWild() && snapshot.CurrentColor != top.Color {
		return invalidSnapshot("current color does not match discard top")
	}
	return nil
}

// validateSnapshotCards 校验每张实体牌的标准身份、唯一性与总量守恒。
func validateSnapshotCards(snapshot Snapshot) error {
	canonicalDeck := NewStandardDeck()
	seenCards := make(map[CardID]struct{}, StandardDeckSize)
	// 除校验 ID 外还与标准牌库逐字段比对，避免合法 ID 被篡改成其他牌面。
	addCards := func(location string, cards []Card) error {
		for _, card := range cards {
			if !card.Valid() || int(card.ID) > StandardDeckSize {
				return invalidSnapshot("%s has invalid card %+v", location, card)
			}
			if card != canonicalDeck[int(card.ID)-1] {
				return invalidSnapshot("%s has noncanonical card %+v", location, card)
			}
			if _, exists := seenCards[card.ID]; exists {
				return invalidSnapshot("duplicate card ID %d", card.ID)
			}
			seenCards[card.ID] = struct{}{}
		}
		return nil
	}
	if err := addCards("draw pile", snapshot.DrawPile); err != nil {
		return err
	}
	if err := addCards("discard pile", snapshot.DiscardPile); err != nil {
		return err
	}
	for seat, player := range snapshot.Players {
		if err := addCards(fmt.Sprintf("seat %d", seat), player.Hand); err != nil {
			return err
		}
	}
	if len(seenCards) != StandardDeckSize {
		return invalidSnapshot("card count %d", len(seenCards))
	}
	return nil
}

// validateSnapshotPendingActions 校验摸牌决策、万能牌选色和 UNO 抓罚子状态。
func validateSnapshotPendingActions(snapshot Snapshot) error {
	if snapshot.Phase == PhaseAwaitingDrawDecision &&
		!snapshotHandContains(snapshot, snapshot.CurrentSeat, snapshot.DrawnCardID) {
		return invalidSnapshot("drawn card is not in current hand")
	}
	if snapshot.PendingColor != nil {
		chooserSeat := snapshotPlayerSeat(snapshot, snapshot.PendingColor.PlayerID)
		if chooserSeat < 0 || chooserSeat != snapshot.CurrentSeat {
			return invalidSnapshot("invalid color chooser")
		}
		top := snapshot.DiscardPile[len(snapshot.DiscardPile)-1]
		if !top.IsWild() || top != snapshot.PendingColor.Card {
			return invalidSnapshot("pending color card is not discard top")
		}
		if snapshot.PendingColor.Opening && top.Kind != KindWild {
			return invalidSnapshot("opening color choice is not a plain wild")
		}
	}
	seenChallenges := make(map[int64]struct{}, len(snapshot.UNOChallenges))
	// 只有恰好剩一张牌的在座玩家才能拥有非零截止时间的挑战记录。
	for _, challenge := range snapshot.UNOChallenges {
		seat := snapshotPlayerSeat(snapshot, challenge.PlayerID)
		if seat < 0 || len(snapshot.Players[seat].Hand) != 1 || challenge.ExpiresAt.IsZero() {
			return invalidSnapshot("invalid UNO challenge for player %d", challenge.PlayerID)
		}
		if _, duplicate := seenChallenges[challenge.PlayerID]; duplicate {
			return invalidSnapshot("duplicate UNO challenge for player %d", challenge.PlayerID)
		}
		seenChallenges[challenge.PlayerID] = struct{}{}
	}
	return nil
}

// validateRoundResult 根据权威手牌重新计算并核对终局结算。
func validateRoundResult(snapshot Snapshot) error {
	if len(snapshot.WinnerCandidates) == 0 {
		return fmt.Errorf("winner candidate is missing")
	}
	winnerSeat := snapshot.WinnerCandidates[0]
	winnerID := snapshot.Players[winnerSeat].PlayerID
	if snapshot.Result.WinnerID != winnerID || len(snapshot.Result.Players) != len(snapshot.Players) {
		return fmt.Errorf("winner or player count mismatch")
	}

	winnerScore := 0
	// 逐座位核对身份、胜负标志、剩余张数和手牌分值。
	for seat, player := range snapshot.Players {
		handPoints := 0
		for _, card := range player.Hand {
			handPoints += card.Points()
		}
		if seat != winnerSeat {
			winnerScore += handPoints
		}
		result := snapshot.Result.Players[seat]
		if result.PlayerID != player.PlayerID ||
			result.IsWinner != (seat == winnerSeat) ||
			result.HandPoints != handPoints ||
			result.CardsLeft != len(player.Hand) {
			return fmt.Errorf("seat %d settlement mismatch", seat)
		}
	}
	if snapshot.Result.Score != winnerScore {
		return fmt.Errorf("winner score %d, want %d", snapshot.Result.Score, winnerScore)
	}
	// 胜者获得其他玩家手牌总分，非胜者得分必须为零。
	for seat, result := range snapshot.Result.Players {
		wantScore := 0
		if seat == winnerSeat {
			wantScore = winnerScore
		}
		if result.Score != wantScore {
			return fmt.Errorf("seat %d score %d, want %d", seat, result.Score, wantScore)
		}
	}
	return nil
}

// validPhase 判断阶段值是否属于当前状态机。
func validPhase(phase Phase) bool {
	switch phase {
	case PhasePlaying, PhaseAwaitingColor, PhaseAwaitingDrawDecision, PhaseFinished:
		return true
	default:
		return false
	}
}

// snapshotPlayerSeat 按持久化座位顺序查找玩家，未找到时返回 -1。
func snapshotPlayerSeat(snapshot Snapshot, playerID int64) int {
	for seat, player := range snapshot.Players {
		if player.PlayerID == playerID {
			return seat
		}
	}
	return -1
}

// snapshotHandContains 判断指定快照手牌是否持有某张实体牌。
func snapshotHandContains(snapshot Snapshot, seat int, cardID CardID) bool {
	for _, card := range snapshot.Players[seat].Hand {
		if card.ID == cardID {
			return true
		}
	}
	return false
}
