package server

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luanchang-zh/UNO-Server/internal/auth"
	"github.com/luanchang-zh/UNO-Server/internal/config"
	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
)

// TestRoom_TimeoutAutoPlayFinishesGame 验证双方无人操作时会依次超时、进入托管并完成整局。
func TestRoom_TimeoutAutoPlayFinishesGame(t *testing.T) {
	srv, testServer, authService := newLifecycleTestServer(t, config.Config{
		TurnTimeout:        10 * time.Millisecond,
		ManagedActionDelay: 2 * time.Millisecond,
		TimeoutStrikeLimit: 1,
	})
	ownerConn, ownerID := dialPlayerWithID(t, testServer, authService, "超时房主")
	defer ownerConn.Close()
	guestConn, guestID := dialPlayerWithID(t, testServer, authService, "超时玩家")
	defer guestConn.Close()
	readEnvelope(t, ownerConn)
	readEnvelope(t, guestConn)
	startTwoPlayerGame(t, ownerConn, guestConn)

	ownerInitial := readGameStateWithDeadline(t, ownerConn)
	guestInitial := readGameStateWithDeadline(t, guestConn)
	if ownerInitial.TurnDeadline.IsZero() || guestInitial.TurnDeadline.IsZero() {
		t.Fatalf("开局状态缺少回合截止时间：owner=%v guest=%v", ownerInitial.TurnDeadline, guestInitial.TurnDeadline)
	}
	views := map[int64]uno.View{ownerID: ownerInitial.View, guestID: guestInitial.View}
	initialActorID := views[ownerID].CurrentPlayerID
	if views[ownerID].Phase == uno.PhaseAwaitingColor {
		initialActorID = views[ownerID].ColorChooserID
	}
	ownerRoomState := readRoomState(t, ownerConn)
	guestRoomState := readRoomState(t, guestConn)
	assertManagedMember(t, ownerRoomState, initialActorID)
	assertManagedMember(t, guestRoomState, initialActorID)

	const maxAutomaticCommands = 5000
	for commandIndex := 0; commandIndex < maxAutomaticCommands; commandIndex++ {
		views[ownerID] = readGameState(t, ownerConn)
		views[guestID] = readGameState(t, guestConn)
		assertGameViews(t, views, ownerID, guestID)
		if views[ownerID].Phase == uno.PhaseFinished {
			break
		}
	}
	if views[ownerID].Phase != uno.PhaseFinished || views[ownerID].Result == nil {
		t.Fatalf("全托管牌局未结束：%+v", views[ownerID])
	}
	if srv.rooms.Count() != 1 {
		t.Fatalf("托管结束后房间数=%d", srv.rooms.Count())
	}
}

// TestRoom_ReconnectRebindsSession 验证对局中断线保留座位，同一 token 重连后恢复私有视图和操作权。
func TestRoom_ReconnectRebindsSession(t *testing.T) {
	_, testServer, authService := newLifecycleTestServer(t, config.Config{
		TurnTimeout:        5 * time.Second,
		ManagedActionDelay: 5 * time.Second,
		TimeoutStrikeLimit: 2,
	})
	ownerLogin, err := authService.LoginGuest("重连房主")
	if err != nil {
		t.Fatalf("房主登录失败：%v", err)
	}
	guestLogin, err := authService.LoginGuest("重连玩家")
	if err != nil {
		t.Fatalf("玩家登录失败：%v", err)
	}
	ownerID := ownerLogin.Player.ID
	guestID := guestLogin.Player.ID
	ownerConn := dialPlayerWithToken(t, testServer, ownerLogin.Token)
	guestConn := dialPlayerWithToken(t, testServer, guestLogin.Token)
	defer guestConn.Close()
	readEnvelope(t, ownerConn)
	readEnvelope(t, guestConn)
	startTwoPlayerGame(t, ownerConn, guestConn)
	views := map[int64]uno.View{
		ownerID: readGameState(t, ownerConn),
		guestID: readGameState(t, guestConn),
	}

	disconnectedID := views[ownerID].CurrentPlayerID
	if views[ownerID].Phase == uno.PhaseAwaitingColor {
		disconnectedID = views[ownerID].ColorChooserID
	}
	disconnectedConn := ownerConn
	disconnectedToken := ownerLogin.Token
	observerConn := guestConn
	if disconnectedID == guestID {
		disconnectedConn = guestConn
		disconnectedToken = guestLogin.Token
		observerConn = ownerConn
		defer ownerConn.Close()
	}
	if err := disconnectedConn.Close(); err != nil {
		t.Fatalf("关闭旧连接失败：%v", err)
	}
	disconnectedState := readRoomState(t, observerConn)
	disconnectedMember := findRoomMember(t, disconnectedState, disconnectedID)
	if disconnectedMember.Connected || !disconnectedMember.AutoPlay {
		t.Fatalf("断线成员状态不正确：%+v", disconnectedMember)
	}
	disconnectedGameState := readGameStateWithDeadline(t, observerConn)
	if disconnectedGameState.TurnDeadline.IsZero() {
		t.Fatal("当前玩家断线后观察者未收到新的托管截止时间")
	}

	reconnectedConn := dialPlayerWithToken(t, testServer, disconnectedToken)
	defer reconnectedConn.Close()
	hello := readEnvelope(t, reconnectedConn)
	if hello.Type != protocol.TypeHello {
		t.Fatalf("重连首包=%s，期望 hello", hello.Type)
	}
	reconnectedRoomState := readRoomState(t, reconnectedConn)
	reconnectedMember := findRoomMember(t, reconnectedRoomState, disconnectedID)
	if !reconnectedMember.Connected || reconnectedMember.AutoPlay || reconnectedMember.TimeoutStrikes != 0 {
		t.Fatalf("重连成员状态不正确：%+v", reconnectedMember)
	}
	observerRoomState := readRoomState(t, observerConn)
	observerMember := findRoomMember(t, observerRoomState, disconnectedID)
	if !observerMember.Connected || observerMember.AutoPlay {
		t.Fatalf("观察者收到的重连成员状态不正确：%+v", observerMember)
	}
	reconnectedPayload := readGameStateWithDeadline(t, reconnectedConn)
	if reconnectedPayload.TurnDeadline.IsZero() {
		t.Fatal("重连全量状态缺少回合截止时间")
	}
	observerPayload := readGameStateWithDeadline(t, observerConn)
	if !observerPayload.TurnDeadline.Equal(reconnectedPayload.TurnDeadline) {
		t.Fatalf(
			"重连后双方截止时间不一致：reconnected=%v observer=%v",
			reconnectedPayload.TurnDeadline,
			observerPayload.TurnDeadline,
		)
	}
	reconnectedView := reconnectedPayload.View
	assertPrivateHand(t, reconnectedView, disconnectedID)

	connections := map[int64]*websocket.Conn{
		ownerID: ownerConn,
		guestID: guestConn,
	}
	connections[disconnectedID] = reconnectedConn
	views[disconnectedID] = reconnectedView
	views[otherPlayerID(ownerID, guestID, disconnectedID)] = observerPayload.View
	sendAutomatedGameCommand(t, connections, views, ownerID, 0, true)
	views[disconnectedID] = readGameState(t, reconnectedConn)
	views[otherPlayerID(ownerID, guestID, disconnectedID)] = readGameState(t, observerConn)
	assertGameViews(t, views, ownerID, guestID)
}

// newLifecycleTestServer 创建带短生命周期参数的房间集成测试服务。
func newLifecycleTestServer(
	t *testing.T,
	runtimeConfig config.Config,
) (*Server, *httptest.Server, *auth.Service) {
	t.Helper()
	logger := logx.NewFromSlog(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	runtimeConfig.HTTPAddr = ":0"
	runtimeConfig.ReadTimeout = 2 * time.Second
	runtimeConfig.WriteTimeout = 2 * time.Second
	runtimeConfig.ShutdownTimeout = 2 * time.Second
	runtimeConfig.TokenTTL = time.Hour
	runtimeConfig.MaxNicknameLen = 32
	authService := auth.NewService(auth.Options{TokenTTL: time.Hour, MaxNicknameLen: 32})
	srv := New(runtimeConfig, authService, logger)
	testServer := httptest.NewServer(srv.httpServer.Handler)
	t.Cleanup(testServer.Close)
	return srv, testServer, authService
}

// startTwoPlayerGame 完成建房、加入、准备和开局，并消费开局房间广播。
func startTwoPlayerGame(t *testing.T, ownerConn, guestConn *websocket.Conn) {
	t.Helper()
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
}

// assertManagedMember 校验指定成员已经因超时进入托管。
func assertManagedMember(t *testing.T, state protocol.RoomStatePayload, playerID int64) {
	t.Helper()
	member := findRoomMember(t, state, playerID)
	if !member.Connected || !member.AutoPlay || member.TimeoutStrikes < 1 {
		t.Fatalf("成员未按预期进入托管：%+v", member)
	}
}

// findRoomMember 从房间公开状态中查找指定玩家。
func findRoomMember(t *testing.T, state protocol.RoomStatePayload, playerID int64) protocol.RoomMemberView {
	t.Helper()
	for _, member := range state.Members {
		if member.PlayerID == playerID {
			return member
		}
	}
	t.Fatalf("房间状态中缺少玩家 %d：%+v", playerID, state)
	return protocol.RoomMemberView{}
}

// otherPlayerID 返回双人局中目标玩家之外的另一名玩家。
func otherPlayerID(ownerID, guestID, targetID int64) int64 {
	if targetID == ownerID {
		return guestID
	}
	return ownerID
}
