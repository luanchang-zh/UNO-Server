package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/luanchang-zh/UNO-Server/internal/protocol"
)

// TestWebSocket_HelloAndPing 验证鉴权升级、hello 与 ping/pong。
func TestWebSocket_HelloAndPing(t *testing.T) {
	srv := newTestServer(io.Discard)
	testServer := httptest.NewServer(srv.httpServer.Handler)
	defer testServer.Close()

	loginResult, err := srv.auth.LoginGuest("WS玩家")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/ws?token=" + loginResult.Token
	conn, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade status=%d", response.StatusCode)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, helloRaw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	hello, err := protocol.Decode(helloRaw)
	if err != nil || hello.Type != protocol.TypeHello {
		t.Fatalf("hello invalid: %s err=%v", helloRaw, err)
	}
	var helloPayload protocol.HelloPayload
	if err := json.Unmarshal(hello.Payload, &helloPayload); err != nil {
		t.Fatalf("hello payload: %v", err)
	}
	if helloPayload.PlayerID != loginResult.Player.ID || helloPayload.Nickname != "WS玩家" {
		t.Fatalf("hello payload mismatch: %+v", helloPayload)
	}

	ping, err := protocol.NewEnvelope(protocol.TypePing, "r1", map[string]int{"t": 7})
	if err != nil {
		t.Fatalf("ping envelope: %v", err)
	}
	pingData, _ := protocol.Encode(ping)
	if err := conn.WriteMessage(websocket.TextMessage, pingData); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, pongRaw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	pong, err := protocol.Decode(pongRaw)
	if err != nil || pong.Type != protocol.TypePong || pong.RequestID != "r1" {
		t.Fatalf("pong invalid: %s err=%v", pongRaw, err)
	}
}

// TestWebSocket_RejectBadToken 验证无效 token 不能升级。
func TestWebSocket_RejectBadToken(t *testing.T) {
	srv := newTestServer(io.Discard)
	testServer := httptest.NewServer(srv.httpServer.Handler)
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/ws?token=bad"
	_, response, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("期望 dial 失败")
	}
	if response == nil || response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("期望 401，实际 response=%v err=%v", response, err)
	}
}

// TestWebSocket_RegisterAndClose 验证连接登记与主动关闭后计数归零。
func TestWebSocket_RegisterAndClose(t *testing.T) {
	srv := newTestServer(io.Discard)
	testServer := httptest.NewServer(srv.httpServer.Handler)
	defer testServer.Close()

	loginResult, err := srv.auth.LoginGuest("登记")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http") + "/ws?token=" + loginResult.Token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// handler 立即返回，登记应很快可见。
	deadline := time.Now().Add(time.Second)
	for srv.sessions.Count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.sessions.Count() != 1 {
		t.Fatalf("期望登记 1 条连接，实际 %d", srv.sessions.Count())
	}

	_ = conn.Close()
	deadline = time.Now().Add(time.Second)
	for srv.sessions.Count() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.sessions.Count() != 0 {
		t.Fatalf("关闭后期望 0 连接，实际 %d", srv.sessions.Count())
	}
}
