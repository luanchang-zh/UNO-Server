package mysql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
)

// TestNormalizeDSN 验证时间、时区和字符集连接参数会被统一补齐。
func TestNormalizeDSN(t *testing.T) {
	dsn, err := normalizeDSN("uno:secret@tcp(127.0.0.1:3306)/uno")
	if err != nil {
		t.Fatalf("规范化 DSN 失败：%v", err)
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("重新解析 DSN 失败：%v", err)
	}
	if !parsed.ParseTime || parsed.Loc != time.UTC {
		t.Fatalf("时间参数不正确：parse_time=%v loc=%v", parsed.ParseTime, parsed.Loc)
	}
	if parsed.Collation != "utf8mb4_unicode_ci" {
		t.Fatalf("连接排序规则=%s", parsed.Collation)
	}
	if parsed.Params["time_zone"] != "'+00:00'" {
		t.Fatalf("会话时区=%q", parsed.Params["time_zone"])
	}
}

// TestRepositoryMigrate 验证三张表按固定顺序执行幂等建表语句。
func TestRepositoryMigrate(t *testing.T) {
	repository, mock, closeDB := newMockRepository(t)
	defer closeDB()
	for _, statement := range schemaStatements {
		mock.ExpectExec(statement).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	if err := repository.Migrate(context.Background()); err != nil {
		t.Fatalf("执行迁移失败：%v", err)
	}
	assertMockExpectations(t, mock)
}

// TestRepositoryCreatePlayerAndMatch 验证玩家与开局元数据使用参数化语句写入。
func TestRepositoryCreatePlayerAndMatch(t *testing.T) {
	repository, mock, closeDB := newMockRepository(t)
	defer closeDB()
	now := time.Date(2026, 8, 8, 12, 0, 0, 123000000, time.UTC)
	player := entity.Player{
		ID:          101,
		Nickname:    "持久化玩家",
		CreatedAt:   now,
		UpdatedAt:   now,
		LastLoginAt: now,
	}
	mock.ExpectExec(insertPlayerQuery).
		WithArgs(player.ID, player.Nickname, now, now, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.CreatePlayer(context.Background(), player); err != nil {
		t.Fatalf("写入玩家失败：%v", err)
	}

	match := entity.Match{
		ID:          201,
		RoomID:      "ABC123",
		Status:      entity.MatchStatusPlaying,
		PlayerCount: 2,
		StartedAt:   now,
		CreatedAt:   now,
	}
	mock.ExpectExec(createMatchQuery).
		WithArgs(match.ID, match.RoomID, string(match.Status), match.PlayerCount, nil, now, nil, now).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repository.CreateMatch(context.Background(), match); err != nil {
		t.Fatalf("写入对局失败：%v", err)
	}
	assertMockExpectations(t, mock)
}

// TestRepositoryFinishMatchCommits 验证对局终态和全部玩家结果在同一事务中提交。
func TestRepositoryFinishMatchCommits(t *testing.T) {
	repository, mock, closeDB := newMockRepository(t)
	defer closeDB()
	match, results := settlementFixture()

	mock.ExpectBegin()
	mock.ExpectExec(finishMatchQuery).
		WithArgs(
			string(entity.MatchStatusFinished),
			*match.WinnerPlayerID,
			*match.EndedAt,
			match.ID,
			string(entity.MatchStatusPlaying),
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	prepared := mock.ExpectPrepare(insertMatchResultQuery)
	for _, result := range results {
		prepared.ExpectExec().WithArgs(
			result.ID,
			result.MatchID,
			result.PlayerID,
			result.SeatIndex,
			result.IsWinner,
			result.Score,
			result.HandPoints,
			result.CardsLeft,
			result.CreatedAt,
		).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()
	if err := repository.FinishMatch(context.Background(), match, results); err != nil {
		t.Fatalf("提交结算失败：%v", err)
	}
	assertMockExpectations(t, mock)
}

// TestRepositoryFinishMatchRollsBack 验证任一结果写入失败时整个结算事务回滚。
func TestRepositoryFinishMatchRollsBack(t *testing.T) {
	repository, mock, closeDB := newMockRepository(t)
	defer closeDB()
	match, results := settlementFixture()

	mock.ExpectBegin()
	mock.ExpectExec(finishMatchQuery).
		WillReturnResult(sqlmock.NewResult(0, 1))
	prepared := mock.ExpectPrepare(insertMatchResultQuery)
	prepared.ExpectExec().WillReturnError(errors.New("模拟结果写入失败"))
	mock.ExpectRollback()
	if err := repository.FinishMatch(context.Background(), match, results); err == nil {
		t.Fatal("结果写入失败后未返回错误")
	}
	assertMockExpectations(t, mock)
}

// TestValidateSettlementRejectsMismatch 验证不完整或身份不一致的结算不会开启事务。
func TestValidateSettlementRejectsMismatch(t *testing.T) {
	match, results := settlementFixture()
	results[1].MatchID++
	if err := validateSettlement(match, results); err == nil {
		t.Fatal("错误 match_id 的结算未被拒绝")
	}
}

// newMockRepository 创建使用严格等值查询匹配的模拟 Repository。
func newMockRepository(t *testing.T) (*Repository, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatalf("创建 SQL 模拟失败：%v", err)
	}
	repository, err := New(db)
	if err != nil {
		_ = db.Close()
		t.Fatalf("创建 Repository 失败：%v", err)
	}
	return repository, mock, func() { _ = db.Close() }
}

// assertMockExpectations 校验所有预期 SQL 均已执行。
func assertMockExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 预期未满足：%v", err)
	}
}

// settlementFixture 构造一局双人终局记录。
func settlementFixture() (entity.Match, []entity.MatchResult) {
	startedAt := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	endedAt := startedAt.Add(3 * time.Minute)
	winnerID := int64(101)
	match := entity.Match{
		ID:             201,
		RoomID:         "ABC123",
		Status:         entity.MatchStatusFinished,
		PlayerCount:    2,
		WinnerPlayerID: &winnerID,
		StartedAt:      startedAt,
		EndedAt:        &endedAt,
		CreatedAt:      startedAt,
	}
	results := []entity.MatchResult{
		{
			ID: 301, MatchID: match.ID, PlayerID: winnerID, SeatIndex: 0,
			IsWinner: true, Score: 15, HandPoints: 0, CardsLeft: 0, CreatedAt: endedAt,
		},
		{
			ID: 302, MatchID: match.ID, PlayerID: 102, SeatIndex: 1,
			Score: 0, HandPoints: 15, CardsLeft: 3, CreatedAt: endedAt,
		},
	}
	return match, results
}
