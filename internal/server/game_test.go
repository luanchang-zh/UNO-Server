package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luanchang-zh/UNO-Server/internal/auth"
	"github.com/luanchang-zh/UNO-Server/internal/config"
	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
)

// TestRoom_PlayCompleteGame 通过两个真实 WebSocket 客户端完成一整局牌局。
func TestRoom_PlayCompleteGame(t *testing.T) {
	logger := logx.NewFromSlog(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	cfg := config.Config{
		HTTPAddr:        ":0",
		ReadTimeout:     2 * time.Second,
		WriteTimeout:    2 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		TokenTTL:        time.Hour,
		MaxNicknameLen:  32,
	}
	authService := auth.NewService(auth.Options{TokenTTL: time.Hour, MaxNicknameLen: 32})
	srv := New(cfg, authService, logger)
	testServer := httptest.NewServer(srv.httpServer.Handler)
	defer testServer.Close()

	ownerConn, ownerID := dialPlayerWithID(t, testServer, authService, "房主")
	defer ownerConn.Close()
	guestConn, guestID := dialPlayerWithID(t, testServer, authService, "玩家乙")
	defer guestConn.Close()
	connections := map[int64]*websocket.Conn{ownerID: ownerConn, guestID: guestConn}

	// 消费鉴权成功后的身份确认消息。
	readEnvelope(t, ownerConn)
	readEnvelope(t, guestConn)

	writeWS(t, ownerConn, protocol.TypeCreateRoom, "create", protocol.CreateRoomPayload{MaxPlayers: 2})
	roomID := readRoomState(t, ownerConn).RoomID
	writeWS(t, guestConn, protocol.TypeJoinRoom, "join", protocol.JoinRoomPayload{RoomID: roomID})
	_ = readRoomState(t, ownerConn)
	_ = readRoomState(t, guestConn)
	writeWS(t, guestConn, protocol.TypeReady, "ready", protocol.ReadyPayload{Ready: true})
	_ = readRoomState(t, ownerConn)
	_ = readRoomState(t, guestConn)
	writeWS(t, ownerConn, protocol.TypeStart, "start", nil)
	_ = readRoomState(t, ownerConn)
	_ = readRoomState(t, guestConn)

	views := map[int64]uno.View{
		ownerID: readGameState(t, ownerConn),
		guestID: readGameState(t, guestConn),
	}
	assertGameViews(t, views, ownerID, guestID)

	// 非当前行动者不能修改牌局，错误命令也不会触发新的状态广播。
	reference := views[ownerID]
	actorID := reference.CurrentPlayerID
	if reference.Phase == uno.PhaseAwaitingColor {
		actorID = reference.ColorChooserID
	}
	offenderID := ownerID
	if offenderID == actorID {
		offenderID = guestID
	}
	if reference.Phase == uno.PhaseAwaitingColor {
		writeWS(t, connections[offenderID], protocol.TypeChooseColor, "wrong-turn", protocol.ChooseColorPayload{Color: string(uno.ColorRed)})
	} else if reference.Phase == uno.PhaseAwaitingDrawDecision {
		writeWS(t, connections[offenderID], protocol.TypePass, "wrong-turn", nil)
	} else {
		writeWS(t, connections[offenderID], protocol.TypeDrawCard, "wrong-turn", nil)
	}
	errorEnvelope, errorPayload := readProtocolError(t, connections[offenderID])
	if errorEnvelope.RequestID != "wrong-turn" || errorPayload.Code != errs.CodeNotYourTurn {
		t.Fatalf("越权错误不匹配：envelope=%+v payload=%+v", errorEnvelope, errorPayload)
	}
	writeWS(t, ownerConn, protocol.TypeCallUNO, "no-uno", nil)
	errorEnvelope, errorPayload = readProtocolError(t, ownerConn)
	if errorEnvelope.RequestID != "no-uno" || errorPayload.Code != errs.CodeNoUNOChallenge {
		t.Fatalf("UNO 错误不匹配：envelope=%+v payload=%+v", errorEnvelope, errorPayload)
	}

	const maxCommands = 5000
	unoCatchExercised := false
	for commandIndex := 0; commandIndex < maxCommands; commandIndex++ {
		if views[ownerID].Phase == uno.PhaseFinished {
			break
		}
		missedUNOPlayerID := int64(0)
		if !unoCatchExercised {
			missedUNOPlayerID = playerReadyToMissUNO(views, ownerID)
		}
		sendAutomatedGameCommand(t, connections, views, ownerID, commandIndex, missedUNOPlayerID == 0)
		views[ownerID] = readGameState(t, ownerConn)
		views[guestID] = readGameState(t, guestConn)
		assertGameViews(t, views, ownerID, guestID)
		if missedUNOPlayerID != 0 {
			assertUNOChallenge(t, views[ownerID], missedUNOPlayerID)
			catcherID := ownerID
			if catcherID == missedUNOPlayerID {
				catcherID = guestID
			}
			cardsBeforeCatch := len(views[missedUNOPlayerID].Hand)
			writeWS(t, connections[catcherID], protocol.TypeCatchUNO, "catch-uno", protocol.CatchUNOPayload{PlayerID: missedUNOPlayerID})
			views[ownerID] = readGameState(t, ownerConn)
			views[guestID] = readGameState(t, guestConn)
			assertGameViews(t, views, ownerID, guestID)
			if got := len(views[missedUNOPlayerID].Hand); got != cardsBeforeCatch+2 {
				t.Fatalf("UNO 抓罚后手牌数=%d，期望=%d", got, cardsBeforeCatch+2)
			}
			unoCatchExercised = true
		}
	}

	ownerView := views[ownerID]
	guestView := views[guestID]
	if ownerView.Phase != uno.PhaseFinished || guestView.Phase != uno.PhaseFinished {
		t.Fatalf("牌局未在命令上限内结束：owner=%s guest=%s", ownerView.Phase, guestView.Phase)
	}
	if ownerView.Result == nil || guestView.Result == nil || ownerView.Result.WinnerID == 0 {
		t.Fatalf("终局结算缺失：owner=%+v guest=%+v", ownerView.Result, guestView.Result)
	}
	if !reflect.DeepEqual(ownerView.Result, guestView.Result) {
		t.Fatalf("双方终局结算不一致：owner=%+v guest=%+v", ownerView.Result, guestView.Result)
	}
	if !unoCatchExercised {
		t.Fatal("完整一局中未覆盖漏喊 UNO 与抓罚流程")
	}
}

// sendAutomatedGameCommand 根据当前玩家的服务端合法牌提示推进一步。
func sendAutomatedGameCommand(
	t *testing.T,
	connections map[int64]*websocket.Conn,
	views map[int64]uno.View,
	referencePlayerID int64,
	commandIndex int,
	sayUNO bool,
) {
	t.Helper()
	reference := views[referencePlayerID]
	actorID := reference.CurrentPlayerID
	if reference.Phase == uno.PhaseAwaitingColor {
		actorID = reference.ColorChooserID
	}
	actorView, found := views[actorID]
	if !found {
		t.Fatalf("找不到行动玩家 %d 的视图：%+v", actorID, reference)
	}
	conn := connections[actorID]
	requestID := fmt.Sprintf("game-%d", commandIndex)

	switch reference.Phase {
	case uno.PhaseAwaitingColor:
		writeWS(t, conn, protocol.TypeChooseColor, requestID, protocol.ChooseColorPayload{Color: string(uno.ColorBlue)})
	case uno.PhaseAwaitingDrawDecision:
		if len(actorView.PlayableCardIDs) == 0 {
			writeWS(t, conn, protocol.TypePass, requestID, nil)
			return
		}
		writeWS(t, conn, protocol.TypePlayCard, requestID, protocol.PlayCardPayload{
			CardID: uint16(actorView.PlayableCardIDs[0]),
			SayUNO: sayUNO,
		})
	case uno.PhasePlaying:
		if len(actorView.PlayableCardIDs) == 0 {
			writeWS(t, conn, protocol.TypeDrawCard, requestID, nil)
			return
		}
		writeWS(t, conn, protocol.TypePlayCard, requestID, protocol.PlayCardPayload{
			CardID: uint16(actorView.PlayableCardIDs[0]),
			SayUNO: sayUNO,
		})
	default:
		t.Fatalf("无法自动处理牌局阶段 %q", reference.Phase)
	}
}

// playerReadyToMissUNO 返回本次合法出牌后会剩一张牌的玩家 ID。
func playerReadyToMissUNO(views map[int64]uno.View, referencePlayerID int64) int64 {
	reference := views[referencePlayerID]
	if reference.Phase != uno.PhasePlaying && reference.Phase != uno.PhaseAwaitingDrawDecision {
		return 0
	}
	actorView, found := views[reference.CurrentPlayerID]
	if !found || len(actorView.Hand) != 2 || len(actorView.PlayableCardIDs) == 0 {
		return 0
	}
	return reference.CurrentPlayerID
}

// assertUNOChallenge 校验目标玩家出现在公开抓罚窗口中。
func assertUNOChallenge(t *testing.T, view uno.View, targetID int64) {
	t.Helper()
	for _, challenge := range view.UNOChallenges {
		if challenge.PlayerID == targetID {
			return
		}
	}
	t.Fatalf("玩家 %d 漏喊 UNO 后未进入抓罚窗口：%+v", targetID, view.UNOChallenges)
}

// readGameState 忽略同一连接上的其他广播，读取下一条牌局全量视图。
func readGameState(t *testing.T, conn *websocket.Conn) uno.View {
	return readGameStateWithDeadline(t, conn).View
}

// testGameStatePayload 同时解析房间层附加的回合截止时间。
type testGameStatePayload struct {
	uno.View
	TurnDeadline time.Time `json:"turn_deadline"`
}

// readGameStateWithDeadline 读取带房间回合截止时间的牌局视图。
func readGameStateWithDeadline(t *testing.T, conn *websocket.Conn) testGameStatePayload {
	t.Helper()
	for index := 0; index < 10; index++ {
		envelope := readEnvelope(t, conn)
		if envelope.Type == protocol.TypeError {
			t.Fatalf("收到非预期错误：%s", envelope.Payload)
		}
		if envelope.Type != protocol.TypeGameState {
			continue
		}
		var payload testGameStatePayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatalf("解析 game_state 失败：%v", err)
		}
		return payload
	}
	t.Fatal("未收到 game_state")
	return testGameStatePayload{}
}

// readProtocolError 读取下一条协议错误消息。
func readProtocolError(t *testing.T, conn *websocket.Conn) (protocol.Envelope, protocol.ErrorPayload) {
	t.Helper()
	for index := 0; index < 5; index++ {
		envelope := readEnvelope(t, conn)
		if envelope.Type != protocol.TypeError {
			continue
		}
		var payload protocol.ErrorPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatalf("解析 error 失败：%v", err)
		}
		return envelope, payload
	}
	t.Fatal("未收到 error")
	return protocol.Envelope{}, protocol.ErrorPayload{}
}

// assertGameViews 校验双方公开状态一致，同时各自只收到自己的私有手牌。
func assertGameViews(t *testing.T, views map[int64]uno.View, ownerID, guestID int64) {
	t.Helper()
	ownerView := views[ownerID]
	guestView := views[guestID]
	if !reflect.DeepEqual(publicGameView(ownerView), publicGameView(guestView)) {
		t.Fatalf("双方公开牌局状态不一致：owner=%+v guest=%+v", ownerView, guestView)
	}
	assertPrivateHand(t, ownerView, ownerID)
	assertPrivateHand(t, guestView, guestID)
	if ownerView.CurrentPlayerID != ownerID && len(ownerView.PlayableCardIDs) != 0 {
		t.Fatalf("非当前玩家收到可出牌提示：player=%d cards=%v", ownerID, ownerView.PlayableCardIDs)
	}
	if guestView.CurrentPlayerID != guestID && len(guestView.PlayableCardIDs) != 0 {
		t.Fatalf("非当前玩家收到可出牌提示：player=%d cards=%v", guestID, guestView.PlayableCardIDs)
	}
}

// publicGameView 移除单名玩家私有字段，供双方公开视图比较。
func publicGameView(view uno.View) uno.View {
	view.Hand = nil
	view.PlayableCardIDs = nil
	view.DrawnCardID = 0
	return view
}

// assertPrivateHand 校验请求玩家手牌数量与其公开计数一致。
func assertPrivateHand(t *testing.T, view uno.View, playerID int64) {
	t.Helper()
	for _, player := range view.Players {
		if player.PlayerID != playerID {
			continue
		}
		if len(view.Hand) != player.Cards {
			t.Fatalf("玩家 %d 私有手牌数=%d，公开计数=%d", playerID, len(view.Hand), player.Cards)
		}
		return
	}
	t.Fatalf("牌局视图中缺少玩家 %d", playerID)
}
