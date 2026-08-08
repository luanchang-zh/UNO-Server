package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/auth"
	"github.com/luanchang-zh/UNO-Server/internal/config"
	"github.com/luanchang-zh/UNO-Server/internal/game/uno"
	"github.com/luanchang-zh/UNO-Server/internal/idgen"
	"github.com/luanchang-zh/UNO-Server/internal/logx"
	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/protocol"
)

// TestRoom_StartPersistenceFailureKeepsWaiting 验证开局记录失败时不会发布半成品牌局。
func TestRoom_StartPersistenceFailureKeepsWaiting(t *testing.T) {
	logger := logx.NewFromSlog(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	generator, err := idgen.New(8)
	if err != nil {
		t.Fatalf("创建 ID 生成器失败：%v", err)
	}
	cfg := config.Config{
		HTTPAddr:              ":0",
		ReadTimeout:           2 * time.Second,
		ShutdownTimeout:       2 * time.Second,
		TokenTTL:              time.Hour,
		MaxNicknameLen:        32,
		TurnTimeout:           time.Hour,
		ManagedActionDelay:    time.Hour,
		TimeoutStrikeLimit:    2,
		MySQLOperationTimeout: time.Second,
	}
	authService := auth.NewService(auth.Options{
		TokenTTL:       time.Hour,
		MaxNicknameLen: 32,
		IDGenerator:    generator,
	})
	srv := New(cfg, authService, logger, Dependencies{
		IDGenerator:     generator,
		MatchRepository: failingMatchRepository{},
	})
	testServer := httptest.NewServer(srv.httpServer.Handler)
	defer testServer.Close()
	ownerConn, _ := dialPlayerWithID(t, testServer, authService, "失败房主")
	defer ownerConn.Close()
	guestConn, _ := dialPlayerWithID(t, testServer, authService, "失败玩家")
	defer guestConn.Close()
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

	writeWS(t, ownerConn, protocol.TypeStart, "start-fails", nil)
	envelope, payload := readProtocolError(t, ownerConn)
	if envelope.RequestID != "start-fails" || payload.Code != errs.CodeInternal {
		t.Fatalf("开局持久化错误不正确：envelope=%+v payload=%+v", envelope, payload)
	}
	writeWS(t, guestConn, protocol.TypeReady, "cancel-ready", protocol.ReadyPayload{Ready: false})
	ownerState := readRoomState(t, ownerConn)
	guestState := readRoomState(t, guestConn)
	if ownerState.Phase != "waiting" || guestState.Phase != "waiting" {
		t.Fatalf("写库失败后房间阶段改变：owner=%s guest=%s", ownerState.Phase, guestState.Phase)
	}
}

// failingMatchRepository 始终拒绝开局记录，用于验证房间失败原子性。
type failingMatchRepository struct{}

// CreateMatch 返回模拟数据库错误。
func (failingMatchRepository) CreateMatch(context.Context, entity.Match) error {
	return errors.New("模拟开局写入失败")
}

// FinishMatch 在本测试中不应被调用。
func (failingMatchRepository) FinishMatch(context.Context, entity.Match, []entity.MatchResult) error {
	return nil
}

// recordingMatchRepository 记录房间层发出的开局与结算实体。
type recordingMatchRepository struct {
	mu       sync.Mutex
	started  []entity.Match
	finished []recordedSettlement
	settled  chan struct{}
	once     sync.Once
}

// recordedSettlement 保存一次终局事务调用的完整参数副本。
type recordedSettlement struct {
	match   entity.Match
	results []entity.MatchResult
}

// newRecordingMatchRepository 创建带终局完成信号的测试仓储。
func newRecordingMatchRepository() *recordingMatchRepository {
	return &recordingMatchRepository{settled: make(chan struct{})}
}

// CreateMatch 记录开局元数据。
func (r *recordingMatchRepository) CreateMatch(_ context.Context, match entity.Match) error {
	r.mu.Lock()
	r.started = append(r.started, match)
	r.mu.Unlock()
	return nil
}

// FinishMatch 记录终局元数据和逐玩家结果，并通知测试等待者。
func (r *recordingMatchRepository) FinishMatch(
	_ context.Context,
	match entity.Match,
	results []entity.MatchResult,
) error {
	r.mu.Lock()
	r.finished = append(r.finished, recordedSettlement{
		match:   match,
		results: append([]entity.MatchResult(nil), results...),
	})
	r.mu.Unlock()
	r.once.Do(func() { close(r.settled) })
	return nil
}

// assertSettlement 校验完整一局只产生一条对局记录和一组一致结算行。
func (r *recordingMatchRepository) assertSettlement(t *testing.T, roomID string, expected *uno.RoundResult) {
	t.Helper()
	select {
	case <-r.settled:
	case <-time.After(2 * time.Second):
		t.Fatal("等待对局结算持久化超时")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.started) != 1 || len(r.finished) != 1 {
		t.Fatalf("持久化次数不正确：started=%d finished=%d", len(r.started), len(r.finished))
	}
	started := r.started[0]
	settlement := r.finished[0]
	if started.ID <= 0 || started.ID != settlement.match.ID || started.RoomID != roomID {
		t.Fatalf("开局与终局元数据不一致：started=%+v finished=%+v", started, settlement.match)
	}
	if started.Status != entity.MatchStatusPlaying || settlement.match.Status != entity.MatchStatusFinished {
		t.Fatalf("对局状态不正确：started=%s finished=%s", started.Status, settlement.match.Status)
	}
	if settlement.match.WinnerPlayerID == nil || *settlement.match.WinnerPlayerID != expected.WinnerID {
		t.Fatalf("持久化胜者不正确：match=%+v expected=%+v", settlement.match, expected)
	}
	if len(settlement.results) != len(expected.Players) {
		t.Fatalf("结果行数量=%d，期望=%d", len(settlement.results), len(expected.Players))
	}
	seenResultIDs := make(map[int64]struct{}, len(settlement.results))
	for seat, result := range settlement.results {
		expectedPlayer := expected.Players[seat]
		if result.ID <= 0 || result.MatchID != started.ID || int(result.SeatIndex) != seat {
			t.Fatalf("座位 %d 的结果主键或关联不正确：%+v", seat, result)
		}
		if _, duplicate := seenResultIDs[result.ID]; duplicate {
			t.Fatalf("结果 ID 重复：%d", result.ID)
		}
		seenResultIDs[result.ID] = struct{}{}
		if result.PlayerID != expectedPlayer.PlayerID ||
			result.IsWinner != expectedPlayer.IsWinner ||
			result.Score != expectedPlayer.Score ||
			result.HandPoints != expectedPlayer.HandPoints ||
			int(result.CardsLeft) != expectedPlayer.CardsLeft {
			t.Fatalf("座位 %d 的结算明细不一致：got=%+v want=%+v", seat, result, expectedPlayer)
		}
	}
}
