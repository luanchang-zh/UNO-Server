package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
)

// TestLoginGuest_DefaultNickname 验证空昵称回落为默认「游客」。
func TestLoginGuest_DefaultNickname(t *testing.T) {
	service := NewService(Options{TokenTTL: time.Hour, MaxNicknameLen: 32})

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
