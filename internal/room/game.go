package room

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
	"github.com/luanchang-zh/UNO-Server/internal/session"
)

// handleGameCommand 在房间唯一写协程内校验并执行一条牌局命令。
func (r *Room) handleGameCommand(playerSession *session.Session, envelope protocol.Envelope) error {
	if r.Phase != PhasePlaying || r.engine == nil {
		return r.replyError(playerSession, envelope.RequestID, errs.CodeGameNotPlaying, "当前没有进行中的牌局")
	}

	var commandErr error
	switch envelope.Type {
	case protocol.TypePlayCard:
		var payload protocol.PlayCardPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.CardID == 0 {
			return r.replyError(playerSession, envelope.RequestID, errs.CodeInvalidArgument, "play_card payload 非法")
		}
		commandErr = r.engine.Play(playerSession.PlayerID, uno.CardID(payload.CardID), payload.SayUNO)
	case protocol.TypeDrawCard:
		_, commandErr = r.engine.Draw(playerSession.PlayerID)
	case protocol.TypePass:
		commandErr = r.engine.Pass(playerSession.PlayerID)
	case protocol.TypeChooseColor:
		var payload protocol.ChooseColorPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			return r.replyError(playerSession, envelope.RequestID, errs.CodeInvalidArgument, "choose_color payload 非法")
		}
		color := uno.Color(strings.ToLower(strings.TrimSpace(payload.Color)))
		commandErr = r.engine.ChooseColor(playerSession.PlayerID, color)
	case protocol.TypeCallUNO:
		commandErr = r.engine.CallUNO(playerSession.PlayerID)
	case protocol.TypeCatchUNO:
		var payload protocol.CatchUNOPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil || payload.PlayerID <= 0 {
			return r.replyError(playerSession, envelope.RequestID, errs.CodeInvalidArgument, "catch_uno payload 非法")
		}
		_, commandErr = r.engine.CatchUNO(playerSession.PlayerID, payload.PlayerID)
	default:
		return r.replyError(playerSession, envelope.RequestID, errs.CodeInvalidArgument, "不支持的牌局消息类型")
	}

	if commandErr != nil {
		code, message := mapGameError(commandErr)
		return r.replyError(playerSession, envelope.RequestID, code, message)
	}
	return r.finishGameCommand(playerSession, envelope.RequestID)
}

// finishGameCommand 在成功命令后同步房间阶段，并向每名成员广播其专属视图。
func (r *Room) finishGameCommand(playerSession *session.Session, requestID string) error {
	requesterView, err := r.engine.ViewFor(playerSession.PlayerID)
	if err != nil {
		return r.replyError(playerSession, requestID, errs.CodeInternal, "生成牌局视图失败")
	}
	if requesterView.Phase == uno.PhaseFinished {
		r.Phase = PhaseSettled
		r.broadcastState()
	}
	r.broadcastGameState()
	return nil
}

// broadcastGameState 为每名成员生成独立视图，避免向其他玩家泄露私有手牌。
func (r *Room) broadcastGameState() {
	if r.engine == nil {
		return
	}
	for _, member := range r.Members {
		if member.Session == nil {
			continue
		}
		view, err := r.engine.ViewFor(member.PlayerID)
		if err != nil {
			continue
		}
		envelope, err := protocol.NewEnvelope(protocol.TypeGameState, "", view)
		if err != nil {
			continue
		}
		_ = member.Session.SendEnvelope(envelope)
	}
}

// mapGameError 将规则引擎错误转换为稳定的客户端错误码和中文提示。
func mapGameError(err error) (code, message string) {
	switch {
	case errors.Is(err, uno.ErrNotYourTurn):
		return errs.CodeNotYourTurn, "当前未轮到该玩家"
	case errors.Is(err, uno.ErrIllegalCard):
		return errs.CodeIllegalCard, "该牌当前不能打出"
	case errors.Is(err, uno.ErrGameNotPlaying):
		return errs.CodeGameNotPlaying, "当前没有进行中的牌局"
	case errors.Is(err, uno.ErrCardNotFound):
		return errs.CodeCardNotFound, "玩家手中不存在该实体牌"
	case errors.Is(err, uno.ErrActionNotAllowed):
		return errs.CodeActionNotAllowed, "当前阶段不允许该操作"
	case errors.Is(err, uno.ErrInvalidColor):
		return errs.CodeInvalidColor, "万能牌颜色不合法"
	case errors.Is(err, uno.ErrNoUNOChallenge):
		return errs.CodeNoUNOChallenge, "当前没有可处理的 UNO 抓罚"
	case errors.Is(err, uno.ErrCannotCatchSelf):
		return errs.CodeCannotCatchSelf, "不能抓罚自己"
	case errors.Is(err, uno.ErrPlayerNotFound):
		return errs.CodeNotInRoom, "玩家不在当前牌局中"
	default:
		return errs.CodeInternal, "牌局操作失败"
	}
}
