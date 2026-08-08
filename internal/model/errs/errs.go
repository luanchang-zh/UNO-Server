// Package errs 集中定义可跨层使用的哨兵错误与对外错误码字符串。
//
// HTTP/WS 映射状态码时用 errors.Is 判断哨兵错误，响应 body 使用 Code 常量。
package errs

import "errors"

// 通用错误。
var (
	// ErrNotFound 表示资源不存在。
	ErrNotFound = errors.New("not found")
	// ErrInvalidArgument 表示参数不合法。
	ErrInvalidArgument = errors.New("invalid argument")
	// ErrInternal 表示未预期的内部错误。
	ErrInternal = errors.New("internal error")
)

// 鉴权 / 登录相关错误（从 auth 抽离，供 HTTP 与后续 WS 共用）。
var (
	// ErrInvalidNickname 表示昵称不合法。
	ErrInvalidNickname = errors.New("invalid nickname")
	// ErrTokenNotFound 表示 token 不存在。
	ErrTokenNotFound = errors.New("token not found")
	// ErrTokenExpired 表示 token 已过期。
	ErrTokenExpired = errors.New("token expired")
	// ErrUnauthorized 表示未认证或认证失败（统称）。
	ErrUnauthorized = errors.New("unauthorized")
)

// 房间相关错误（M1 起使用，先定义避免多处字符串分叉）。
var (
	// ErrRoomNotFound 表示房间不存在。
	ErrRoomNotFound = errors.New("room not found")
	// ErrRoomFull 表示房间已满。
	ErrRoomFull = errors.New("room full")
	// ErrAlreadyInRoom 表示玩家已在房间中。
	ErrAlreadyInRoom = errors.New("already in room")
	// ErrNotInRoom 表示玩家不在目标房间。
	ErrNotInRoom = errors.New("not in room")
	// ErrNotRoomOwner 表示非房主无权限。
	ErrNotRoomOwner = errors.New("not room owner")
	// ErrRoomNotReady 表示未满足开局条件（人数/准备）。
	ErrRoomNotReady = errors.New("room not ready")
	// ErrRoomAlreadyPlaying 表示房间已在对局中。
	ErrRoomAlreadyPlaying = errors.New("room already playing")
)

// 对局相关错误（M3 起使用）。
var (
	// ErrNotYourTurn 表示非当前回合玩家。
	ErrNotYourTurn = errors.New("not your turn")
	// ErrIllegalCard 表示出牌不合法。
	ErrIllegalCard = errors.New("illegal card")
	// ErrGameNotPlaying 表示当前不在对局阶段。
	ErrGameNotPlaying = errors.New("game not playing")
)

// 对外错误码（JSON error 字段），与哨兵错误一一对应，便于客户端分支。
const (
	CodeNotFound         = "not_found"
	CodeInvalidArgument  = "invalid_argument"
	CodeInternal         = "internal_error"
	CodeInvalidNickname  = "invalid_nickname"
	CodeTokenNotFound    = "token_not_found"
	CodeTokenExpired     = "token_expired"
	CodeUnauthorized     = "unauthorized"
	CodeInvalidJSON      = "invalid_json"
	CodeRoomNotFound     = "room_not_found"
	CodeRoomFull         = "room_full"
	CodeAlreadyInRoom    = "already_in_room"
	CodeNotInRoom        = "not_in_room"
	CodeNotRoomOwner     = "not_room_owner"
	CodeRoomNotReady     = "room_not_ready"
	CodeRoomPlaying      = "room_already_playing"
	CodeNotYourTurn      = "not_your_turn"
	CodeIllegalCard      = "illegal_card"
	CodeGameNotPlaying   = "game_not_playing"
	CodeCardNotFound     = "card_not_found"
	CodeActionNotAllowed = "action_not_allowed"
	CodeInvalidColor     = "invalid_color"
	CodeNoUNOChallenge   = "no_uno_challenge"
	CodeCannotCatchSelf  = "cannot_catch_self"
)
