package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luanchang-zh/UNO-Server/internal/auth"
	"github.com/luanchang-zh/UNO-Server/internal/config"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
	"github.com/luanchang-zh/UNO-Server/internal/room"
)

// TestRoom_CreateJoinReadyStart 验证双人建房、准备与开局状态广播。
func TestRoom_CreateJoinReadyStart(t *testing.T) {
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

	ownerConn := dialPlayer(t, testServer, authService, "房主")
	defer ownerConn.Close()
	guestConn := dialPlayer(t, testServer, authService, "玩家乙")
	defer guestConn.Close()

	// 读掉 hello
	readEnvelope(t, ownerConn)
	readEnvelope(t, guestConn)

	// 房主建房
	writeWS(t, ownerConn, protocol.TypeCreateRoom, "c1", protocol.CreateRoomPayload{MaxPlayers: 4})
	ownerState := readRoomState(t, ownerConn)
	if ownerState.Phase != room.PhaseWaiting || len(ownerState.Members) != 1 || !ownerState.Members[0].IsOwner {
		t.Fatalf("create state: %+v", ownerState)
	}
	roomID := ownerState.RoomID

	// 客人加入
	writeWS(t, guestConn, protocol.TypeJoinRoom, "j1", protocol.JoinRoomPayload{RoomID: roomID})
	stateAfterJoinOwner := readRoomState(t, ownerConn)
	stateAfterJoinGuest := readRoomState(t, guestConn)
	if len(stateAfterJoinOwner.Members) != 2 || len(stateAfterJoinGuest.Members) != 2 {
		t.Fatalf("join members owner=%d guest=%d", len(stateAfterJoinOwner.Members), len(stateAfterJoinGuest.Members))
	}

	// 客人准备
	writeWS(t, guestConn, protocol.TypeReady, "r1", protocol.ReadyPayload{Ready: true})
	_ = readRoomState(t, ownerConn)
	_ = readRoomState(t, guestConn)

	// 房主开局
	writeWS(t, ownerConn, protocol.TypeStart, "s1", nil)
	startedOwner := readRoomState(t, ownerConn)
	startedGuest := readRoomState(t, guestConn)
	if startedOwner.Phase != room.PhasePlaying || startedGuest.Phase != room.PhasePlaying {
		t.Fatalf("start phase owner=%s guest=%s", startedOwner.Phase, startedGuest.Phase)
	}
	if srv.rooms.Count() != 1 {
		t.Fatalf("room count=%d", srv.rooms.Count())
	}
}

// dialPlayer 登录并建立 WebSocket。
func dialPlayer(t *testing.T, testServer *httptest.Server, authService *auth.Service, nickname string) *websocket.Conn {
	t.Helper()
	result, err := authService.LoginGuest(nickname)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/ws?token=" + result.Token
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	return conn
}

// writeWS 发送 WebSocket 协议消息。
func writeWS(t *testing.T, conn *websocket.Conn, typeName, requestID string, payload any) {
	t.Helper()
	envelope, err := protocol.NewEnvelope(typeName, requestID, payload)
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	data, err := protocol.Encode(envelope)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readEnvelope 读取一条消息。
func readEnvelope(t *testing.T, conn *websocket.Conn) protocol.Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	envelope, err := protocol.Decode(raw)
	if err != nil {
		t.Fatalf("decode: %v body=%s", err, raw)
	}
	return envelope
}

// readRoomState 读取直到 room_state。
func readRoomState(t *testing.T, conn *websocket.Conn) protocol.RoomStatePayload {
	t.Helper()
	for i := 0; i < 5; i++ {
		envelope := readEnvelope(t, conn)
		if envelope.Type == protocol.TypeError {
			t.Fatalf("unexpected error: %s", envelope.Payload)
		}
		if envelope.Type != protocol.TypeRoomState {
			continue
		}
		var payload protocol.RoomStatePayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			t.Fatalf("payload: %v", err)
		}
		return payload
	}
	t.Fatal("no room_state")
	return protocol.RoomStatePayload{}
}
