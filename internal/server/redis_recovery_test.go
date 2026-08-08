package server

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gorilla/websocket"

	"github.com/luanchang-zh/UNO-Server/internal/auth"
	"github.com/luanchang-zh/UNO-Server/internal/config"
	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
	"github.com/luanchang-zh/UNO-Server/internal/idgen"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
	redisstore "github.com/luanchang-zh/UNO-Server/internal/store/redis"
)

// TestRedisRecoveryOriginalTokensContinueGame 验证服务重建后原 token、私有手牌和行动权全部恢复。
func TestRedisRecoveryOriginalTokensContinueGame(t *testing.T) {
	redisServer := miniredis.RunT(t)
	repository, err := redisstore.Open(context.Background(), redisstore.Options{Addr: redisServer.Addr()})
	if err != nil {
		t.Fatalf("打开测试 Redis 失败：%v", err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	generator, err := idgen.New(12)
	if err != nil {
		t.Fatalf("创建 ID 生成器失败：%v", err)
	}
	logger := logx.NewFromSlog(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	cfg := config.Config{
		HTTPAddr:              ":0",
		ReadTimeout:           2 * time.Second,
		WriteTimeout:          2 * time.Second,
		ShutdownTimeout:       2 * time.Second,
		TokenTTL:              time.Hour,
		MaxNicknameLen:        32,
		TurnTimeout:           time.Hour,
		ManagedActionDelay:    time.Hour,
		TimeoutStrikeLimit:    2,
		RedisOperationTimeout: time.Second,
		RedisRoomSnapshotTTL:  2 * time.Hour,
	}

	authBefore := auth.NewService(auth.Options{
		TokenTTL:          time.Hour,
		MaxNicknameLen:    32,
		IDGenerator:       generator,
		SessionRepository: repository,
		SessionTimeout:    time.Second,
	})
	serverBefore := New(cfg, authBefore, logger, Dependencies{
		IDGenerator:            generator,
		RoomSnapshotRepository: repository,
	})
	httpBefore := httptest.NewServer(serverBefore.httpServer.Handler)
	ownerLogin, err := authBefore.LoginGuest("恢复房主")
	if err != nil {
		t.Fatalf("房主登录失败：%v", err)
	}
	guestLogin, err := authBefore.LoginGuest("恢复玩家")
	if err != nil {
		t.Fatalf("玩家登录失败：%v", err)
	}
	ownerBefore := dialPlayerWithToken(t, httpBefore, ownerLogin.Token)
	guestBefore := dialPlayerWithToken(t, httpBefore, guestLogin.Token)
	readEnvelope(t, ownerBefore)
	readEnvelope(t, guestBefore)
	startTwoPlayerGame(t, ownerBefore, guestBefore)
	wantViews := map[int64]uno.View{
		ownerLogin.Player.ID: readGameState(t, ownerBefore),
		guestLogin.Player.ID: readGameState(t, guestBefore),
	}
	records, err := repository.LoadRoomSnapshots(context.Background())
	if err != nil || len(records) != 1 {
		t.Fatalf("进行中房间未实时写入 Redis：records=%d err=%v", len(records), err)
	}

	// 测试中停止旧 mailbox 以避免双实例定时器竞争；恢复所依赖的快照在此之前已经存在。
	if err := serverBefore.rooms.Shutdown(context.Background()); err != nil {
		t.Fatalf("停止旧房间失败：%v", err)
	}
	_ = ownerBefore.Close()
	_ = guestBefore.Close()
	httpBefore.Close()

	authAfter := auth.NewService(auth.Options{
		TokenTTL:          time.Hour,
		MaxNicknameLen:    32,
		IDGenerator:       generator,
		SessionRepository: repository,
		SessionTimeout:    time.Second,
	})
	serverAfter := New(cfg, authAfter, logger, Dependencies{
		IDGenerator:            generator,
		RoomSnapshotRepository: repository,
	})
	if err := serverAfter.RestoreRooms(context.Background()); err != nil {
		t.Fatalf("新服务恢复房间失败：%v", err)
	}
	httpAfter := httptest.NewServer(serverAfter.httpServer.Handler)
	t.Cleanup(func() {
		_ = serverAfter.rooms.Shutdown(context.Background())
		httpAfter.Close()
	})

	ownerAfter := dialPlayerWithToken(t, httpAfter, ownerLogin.Token)
	defer ownerAfter.Close()
	if hello := readEnvelope(t, ownerAfter); hello.Type != protocol.TypeHello {
		t.Fatalf("房主恢复首包=%s", hello.Type)
	}
	ownerRoomState := readRoomState(t, ownerAfter)
	ownerRecovered := readGameState(t, ownerAfter)
	if !findRoomMember(t, ownerRoomState, ownerLogin.Player.ID).Connected {
		t.Fatal("房主 token 未重新绑定原座位")
	}

	guestAfter := dialPlayerWithToken(t, httpAfter, guestLogin.Token)
	defer guestAfter.Close()
	if hello := readEnvelope(t, guestAfter); hello.Type != protocol.TypeHello {
		t.Fatalf("玩家恢复首包=%s", hello.Type)
	}
	guestRoomState := readRoomState(t, guestAfter)
	guestRecovered := readGameState(t, guestAfter)
	if !findRoomMember(t, guestRoomState, guestLogin.Player.ID).Connected {
		t.Fatal("玩家 token 未重新绑定原座位")
	}
	// 第二名玩家换绑会向房主广播房间状态；若其正好是行动者，还会同步广播新截止时间。
	_ = readRoomState(t, ownerAfter)
	actorID := wantViews[ownerLogin.Player.ID].CurrentPlayerID
	if wantViews[ownerLogin.Player.ID].Phase == uno.PhaseAwaitingColor {
		actorID = wantViews[ownerLogin.Player.ID].ColorChooserID
	}
	if actorID == guestLogin.Player.ID {
		ownerRecovered = readGameState(t, ownerAfter)
	}

	if !reflect.DeepEqual(ownerRecovered, wantViews[ownerLogin.Player.ID]) ||
		!reflect.DeepEqual(guestRecovered, wantViews[guestLogin.Player.ID]) {
		t.Fatal("重启恢复后公开桌面或玩家私有手牌发生变化")
	}
	connections := map[int64]*websocket.Conn{
		ownerLogin.Player.ID: ownerAfter,
		guestLogin.Player.ID: guestAfter,
	}
	recoveredViews := map[int64]uno.View{
		ownerLogin.Player.ID: ownerRecovered,
		guestLogin.Player.ID: guestRecovered,
	}
	sendAutomatedGameCommand(t, connections, recoveredViews, ownerLogin.Player.ID, 0, true)
	recoveredViews[ownerLogin.Player.ID] = readGameState(t, ownerAfter)
	recoveredViews[guestLogin.Player.ID] = readGameState(t, guestAfter)
	assertGameViews(t, recoveredViews, ownerLogin.Player.ID, guestLogin.Player.ID)
}
