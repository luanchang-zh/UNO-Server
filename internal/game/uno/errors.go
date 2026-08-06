package uno

import (
	"errors"

	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
)

// 下列错误供调用方进行业务分支判断。
var (
	// ErrNotYourTurn 复用项目面向协议层的“非当前回合”哨兵错误。
	ErrNotYourTurn = errs.ErrNotYourTurn
	// ErrIllegalCard 复用项目面向协议层的“非法出牌”哨兵错误。
	ErrIllegalCard = errs.ErrIllegalCard
	// ErrGameNotPlaying 复用项目面向协议层的“对局未进行”哨兵错误。
	ErrGameNotPlaying = errs.ErrGameNotPlaying

	// ErrInvalidPlayers 表示玩家列表无法组成有效对局。
	ErrInvalidPlayers = errors.New("invalid players")
	// ErrPlayerNotFound 表示玩家不在本局座位中。
	ErrPlayerNotFound = errors.New("player not found")
	// ErrCardNotFound 表示指定实体牌不在玩家手中。
	ErrCardNotFound = errors.New("card not found in hand")
	// ErrActionNotAllowed 表示当前阶段不允许执行该命令。
	ErrActionNotAllowed = errors.New("action not allowed")
	// ErrInvalidColor 表示万能牌选择的颜色不属于四种合法颜色。
	ErrInvalidColor = errors.New("invalid color")
	// ErrNoUNOChallenge 表示当前没有可抓罚的漏喊 UNO 玩家。
	ErrNoUNOChallenge = errors.New("no active UNO challenge")
	// ErrCannotCatchSelf 表示玩家尝试抓罚自己。
	ErrCannotCatchSelf = errors.New("cannot catch own UNO")
	// ErrInvalidSnapshot 表示持久化的引擎状态不一致。
	ErrInvalidSnapshot = errors.New("invalid UNO snapshot")
)
