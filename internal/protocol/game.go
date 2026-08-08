package protocol

// 牌局相关消息类型（M3）。
const (
	// TypePlayCard 表示当前玩家打出一张实体牌。
	TypePlayCard = "play_card"
	// TypeDrawCard 表示当前玩家主动摸牌或接受累计罚牌。
	TypeDrawCard = "draw_card"
	// TypePass 表示玩家保留刚摸到的可出牌并结束回合。
	TypePass = "pass"
	// TypeChooseColor 表示万能牌出牌者选择生效颜色。
	TypeChooseColor = "choose_color"
	// TypeCallUNO 表示玩家在抓罚窗口内主动补喊 UNO。
	TypeCallUNO = "call_uno"
	// TypeCatchUNO 表示其他玩家抓罚漏喊 UNO 的目标玩家。
	TypeCatchUNO = "catch_uno"
	// TypeGameState 表示服务端向单名玩家推送安全的牌局全量视图。
	TypeGameState = "game_state"
)

// PlayCardPayload 是出牌请求体。
type PlayCardPayload struct {
	// CardID 是玩家手中要打出的实体牌 ID。
	CardID uint16 `json:"card_id"`
	// SayUNO 表示本次出牌后若剩一张牌，玩家是否同时喊出 UNO。
	SayUNO bool `json:"say_uno"`
}

// ChooseColorPayload 是万能牌选色请求体。
type ChooseColorPayload struct {
	// Color 是 red、yellow、green、blue 之一。
	Color string `json:"color"`
}

// CatchUNOPayload 是抓罚漏喊 UNO 玩家的请求体。
type CatchUNOPayload struct {
	// PlayerID 是被抓罚的目标玩家 ID。
	PlayerID int64 `json:"player_id"`
}
