package uno

import "time"

const (
	// minPlayerCount 和 maxPlayerCount 限定单局允许的玩家人数。
	minPlayerCount = 2
	maxPlayerCount = 6
	// defaultChallengeWindow 是漏喊 UNO 后默认可被抓罚的时长。
	defaultChallengeWindow = 4 * time.Second
	// currentSnapshotVersion 标识当前快照结构版本。
	currentSnapshotVersion = 1
)

// Phase 表示引擎当前允许执行的操作阶段。
type Phase string

const (
	// PhasePlaying 允许当前玩家出牌或摸牌。
	PhasePlaying Phase = "playing"
	// PhaseAwaitingColor 只允许已记录的选色玩家指定万能牌颜色。
	PhaseAwaitingColor Phase = "awaiting_color"
	// PhaseAwaitingDrawDecision 只允许打出刚摸到的牌或选择过牌。
	PhaseAwaitingDrawDecision Phase = "awaiting_draw_decision"
	// PhaseFinished 表示本局已经结算，不再接受玩法命令。
	PhaseFinished Phase = "finished"
)

// Direction 表示座位轮转方向。
type Direction int8

const (
	// DirectionClockwise 按座位下标递增方向轮转。
	DirectionClockwise Direction = 1
	// DirectionCounterClockwise 按座位下标递减方向轮转。
	DirectionCounterClockwise Direction = -1
)

// Config 提供初始庄家设置，以及可替换的随机数和时钟依赖。
type Config struct {
	// DealerSeat 指定庄家座位，并据此计算首位行动玩家和开局牌效果。
	DealerSeat int
	// Shuffle 对牌切片原地洗牌；为 nil 时使用密码学安全随机源。
	Shuffle ShuffleFunc
	// Now 为 UNO 抓罚窗口提供当前时间；为 nil 时使用 time.Now。
	Now func() time.Time
	// UNOChallengeWindow 指定漏喊 UNO 后可被其他玩家抓罚的时长。
	UNOChallengeWindow time.Duration
}

// UNOChallenge 记录剩一张牌但未喊 UNO 的玩家及其抓罚截止时间。
type UNOChallenge struct {
	// PlayerID 是等待自报或被抓罚的玩家 ID。
	PlayerID int64 `json:"player_id"`
	// ExpiresAt 是挑战窗口的绝对截止时间，统一使用 UTC。
	ExpiresAt time.Time `json:"expires_at"`
}

// PlayerResult 表示单名玩家不可变的本局结算结果。
type PlayerResult struct {
	// PlayerID 是结算所属玩家 ID。
	PlayerID int64 `json:"player_id"`
	// IsWinner 表示该玩家是否为本局胜者。
	IsWinner bool `json:"is_winner"`
	// Score 是该玩家本局获得的积分；未获胜时为零。
	Score int `json:"score"`
	// HandPoints 是玩家结算时剩余手牌的总分值。
	HandPoints int `json:"hand_points"`
	// CardsLeft 是玩家结算时剩余的手牌数量。
	CardsLeft int `json:"cards_left"`
}

// RoundResult 在首个空手玩家的末牌效果完全结算后生成。
type RoundResult struct {
	// WinnerID 是最终胜者的玩家 ID。
	WinnerID int64 `json:"winner_id"`
	// Score 是胜者从其他玩家剩余手牌获得的总分。
	Score int `json:"score"`
	// Players 按原始座位顺序保存所有玩家的结算明细。
	Players []PlayerResult `json:"players"`
}

// DrawResult 表示一次摸牌命令的结果，其中 Cards 属于私密信息，房间层只能发送给该玩家。
type DrawResult struct {
	// Cards 是本次实际摸到的实体牌副本。
	Cards []Card `json:"cards"`
	// CanPlay 表示普通摸到的一张牌是否可立即打出。
	CanPlay bool `json:"can_play"`
	// Penalty 表示本次摸牌是否在接受累计罚牌。
	Penalty bool `json:"penalty"`
	// TurnEnded 表示此次命令是否已经结束当前回合。
	TurnEnded bool `json:"turn_ended"`
}

// playerState 保存单个座位的服务端权威私有状态。
type playerState struct {
	// id 是该座位绑定的玩家 ID。
	id int64
	// hand 是玩家持有的实体牌，顺序保持稳定以便客户端展示。
	hand []Card
}

// pendingColor 记录尚未完成选色的万能牌动作。
type pendingColor struct {
	// seat 是唯一有权完成选色的座位。
	seat int
	// card 是已经进入弃牌堆、等待指定颜色的万能牌。
	card Card
	// opening 表示该牌是否为开局翻出的普通万能牌。
	opening bool
}

// Engine 持有一局游戏的全部可变状态。
//
// Engine 本身不保证并发安全，房间 Actor 必须作为唯一写入者串行调用它。
type Engine struct {
	// 座位与牌堆共同构成实体牌的唯一归属关系。
	players     []playerState
	drawPile    []Card
	discardPile []Card

	// 回合游标决定当前行动者、轮转方向与生效颜色。
	dealerSeat   int
	currentSeat  int
	direction    Direction
	currentColor Color

	// 待处理字段描述尚未完成的罚牌、摸牌决策和万能牌选色动作。
	pendingDraw   int
	phase         Phase
	drawnCardID   CardID
	pendingColor  *pendingColor
	unoChallenges map[int64]UNOChallenge

	// 结算字段记录末牌效果期间的候选顺序，以及最终不可变结果。
	winnerCandidates []int
	result           *RoundResult

	// 以下依赖仅在运行时使用，不进入快照。
	shuffle         ShuffleFunc
	now             func() time.Time
	challengeWindow time.Duration
}

// normalizedConfig 填充可选依赖，并校验庄家座位是否合法。
func normalizedConfig(config Config, playerCount int) (Config, error) {
	if config.DealerSeat < 0 || config.DealerSeat >= playerCount {
		return Config{}, ErrInvalidPlayers
	}
	if config.Shuffle == nil {
		config.Shuffle = secureShuffle
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.UNOChallengeWindow <= 0 {
		config.UNOChallengeWindow = defaultChallengeWindow
	}
	return config, nil
}
