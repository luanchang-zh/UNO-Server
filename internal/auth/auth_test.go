package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
)

// TestLoginGuest_DefaultNickname 验证空昵称回落为默认「游客」。
func TestLoginGuest_DefaultNickname(t *testing.T) {
	service := NewService(Options{
		TokenTTL:       time.Hour,
		MaxNicknameLen: 32,
		IDGenerator:    &fixedIDGenerator{next: 1},
	})

	result, err := service.LoginGuest("  ")
	if err != nil {
		t.Fatalf("LoginGuest 返回错误: %v", err)
	}
	if result.Player.Nickname != "游客" {
		t.Fatalf("期望昵称 游客，实际 %q", result.Player.Nickname)
	}
	if result.Player.ID != 1 {
		t.Fatalf("期望 player_id=1，实际 %d", result.Player.ID)
	}
	if result.Token == "" {
		t.Fatal("token 不应为空")
	}
}

// TestLoginGuest_PersistsBeforePublishingSession 验证玩家写库成功后才发布可鉴权的内存 token。
func TestLoginGuest_PersistsBeforePublishingSession(t *testing.T) {
	repository := &recordingPlayerRepository{}
	service := NewService(Options{
		TokenTTL:         time.Hour,
		MaxNicknameLen:   32,
		IDGenerator:      &fixedIDGenerator{next: 42},
		PlayerRepository: repository,
	})
	result, err := service.LoginGuestContext(context.Background(), "持久化玩家")
	if err != nil {
		t.Fatalf("登录失败：%v", err)
	}
	if repository.player.ID != 42 || repository.player.Nickname != "持久化玩家" {
		t.Fatalf("持久化玩家不正确：%+v", repository.player)
	}
	if _, err := service.Authenticate(result.Token); err != nil {
		t.Fatalf("持久化成功后的 token 不可用：%v", err)
	}
}

// TestLoginGuest_DoesNotPublishWhenPersistenceFails 验证写库失败时不会返回半成功登录结果。
func TestLoginGuest_DoesNotPublishWhenPersistenceFails(t *testing.T) {
	repository := &recordingPlayerRepository{err: errors.New("模拟数据库失败")}
	service := NewService(Options{
		TokenTTL:         time.Hour,
		IDGenerator:      &fixedIDGenerator{next: 43},
		PlayerRepository: repository,
	})
	result, err := service.LoginGuestContext(context.Background(), "失败玩家")
	if err == nil {
		t.Fatal("玩家写库失败后登录仍然成功")
	}
	if result.Token != "" || result.Player.ID != 0 {
		t.Fatalf("失败登录泄露半成品结果：%+v", result)
	}
}

// TestLoginGuest_CustomNickname 验证自定义昵称与 token 校验。
func TestLoginGuest_CustomNickname(t *testing.T) {
	service := NewService(Options{TokenTTL: time.Hour, MaxNicknameLen: 32})

	result, err := service.LoginGuest("  小明  ")
	if err != nil {
		t.Fatalf("LoginGuest 返回错误: %v", err)
	}
	if result.Player.Nickname != "小明" {
		t.Fatalf("期望昵称 小明，实际 %q", result.Player.Nickname)
	}

	session, err := service.Authenticate(result.Token)
	if err != nil {
		t.Fatalf("Authenticate 返回错误: %v", err)
	}
	if session.PlayerID != result.Player.ID {
		t.Fatalf("token 绑定的 player_id 不一致")
	}
}

// TestLoginGuest_NicknameTooLong 验证超长昵称被拒绝。
func TestLoginGuest_NicknameTooLong(t *testing.T) {
	service := NewService(Options{TokenTTL: time.Hour, MaxNicknameLen: 4})

	_, err := service.LoginGuest("一二三四五")
	if !errors.Is(err, errs.ErrInvalidNickname) {
		t.Fatalf("期望 ErrInvalidNickname，实际 %v", err)
	}
}

// TestAuthenticate_UnknownToken 验证未知 token 返回未找到。
func TestAuthenticate_UnknownToken(t *testing.T) {
	service := NewService(Options{TokenTTL: time.Hour})

	_, err := service.Authenticate("not-exist")
	if !errors.Is(err, errs.ErrTokenNotFound) {
		t.Fatalf("期望 ErrTokenNotFound，实际 %v", err)
	}
}

// TestAuthenticate_ExpiredToken 验证过期 token 被清理。
func TestAuthenticate_ExpiredToken(t *testing.T) {
	service := NewService(Options{TokenTTL: 20 * time.Millisecond})

	result, err := service.LoginGuest("测试")
	if err != nil {
		t.Fatalf("LoginGuest 返回错误: %v", err)
	}

	time.Sleep(40 * time.Millisecond)
	_, err = service.Authenticate(result.Token)
	if !errors.Is(err, errs.ErrTokenExpired) {
		t.Fatalf("期望 ErrTokenExpired，实际 %v", err)
	}
}

// TestLoginGuest_RejectControlChar 验证控制字符昵称被拒绝。
func TestLoginGuest_RejectControlChar(t *testing.T) {
	service := NewService(Options{TokenTTL: time.Hour, MaxNicknameLen: 32})

	_, err := service.LoginGuest("bad\nname")
	if !errors.Is(err, errs.ErrInvalidNickname) {
		t.Fatalf("期望 ErrInvalidNickname，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "非法字符") {
		t.Fatalf("错误信息应提示非法字符，实际 %v", err)
	}
}

// fixedIDGenerator 为鉴权单测返回可预测的玩家 ID。
type fixedIDGenerator struct {
	next int64
}

// Next 返回预设 ID。
func (g *fixedIDGenerator) Next() (int64, error) {
	return g.next, nil
}

// recordingPlayerRepository 记录最后一次玩家写入，并可注入失败。
type recordingPlayerRepository struct {
	player entity.Player
	err    error
}

// CreatePlayer 实现玩家持久化测试端口。
func (r *recordingPlayerRepository) CreatePlayer(_ context.Context, player entity.Player) error {
	r.player = player
	return r.err
}
