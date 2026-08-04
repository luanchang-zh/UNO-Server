// Package auth 提供游客登录、token 签发与校验（当前为进程内内存实现）。
//
// 后续可将玩家落 MySQL、token 落 Redis，而不改动 HTTP 层调用方式。
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// 预定义业务错误，供 HTTP 层映射状态码。
var (
	// ErrInvalidNickname 表示昵称不合法。
	ErrInvalidNickname = errors.New("invalid nickname")
	// ErrTokenNotFound 表示 token 不存在或已失效。
	ErrTokenNotFound = errors.New("token not found")
	// ErrTokenExpired 表示 token 已过期。
	ErrTokenExpired = errors.New("token expired")
)

// Player 表示已创建的玩家身份。
type Player struct {
	// ID 为玩家唯一标识，后续对应 MySQL players.id。
	ID int64
	// Nickname 为展示昵称，允许重名。
	Nickname string
	// CreatedAt 为首次创建时间（UTC）。
	CreatedAt time.Time
}

// Session 表示一次登录会话（token 与玩家绑定）。
type Session struct {
	// Token 为客户端持有的访问凭证。
	Token string
	// PlayerID 为绑定的玩家 ID。
	PlayerID int64
	// Nickname 为登录时的昵称快照，便于响应直接返回。
	Nickname string
	// ExpiresAt 为过期时间（UTC）。
	ExpiresAt time.Time
}

// LoginResult 为游客登录成功后的返回值。
type LoginResult struct {
	Player    Player
	Token     string
	ExpiresAt time.Time
}

// Options 控制鉴权服务行为。
type Options struct {
	// TokenTTL 为新签发 token 的有效期。
	TokenTTL time.Duration
	// MaxNicknameLen 为昵称最大 Unicode 字符数。
	MaxNicknameLen int
}

// Service 提供登录与 token 校验能力。
type Service struct {
	tokenTTL       time.Duration
	maxNicknameLen int
	nextPlayerID   atomic.Int64

	mu       sync.RWMutex
	players  map[int64]*Player
	sessions map[string]*Session // token -> session
}

// NewService 创建内存版鉴权服务。
func NewService(options Options) *Service {
	if options.TokenTTL <= 0 {
		options.TokenTTL = 24 * time.Hour
	}
	if options.MaxNicknameLen <= 0 {
		options.MaxNicknameLen = 32
	}

	service := &Service{
		tokenTTL:       options.TokenTTL,
		maxNicknameLen: options.MaxNicknameLen,
		players:        make(map[int64]*Player),
		sessions:       make(map[string]*Session),
	}
	// 从 1 起分配，避免 0 被当成「未设置」。
	service.nextPlayerID.Store(1)
	return service
}

// LoginGuest 创建游客玩家并签发 token；nickname 为空时使用默认昵称。
func (s *Service) LoginGuest(nickname string) (LoginResult, error) {
	normalized, err := s.normalizeNickname(nickname)
	if err != nil {
		return LoginResult{}, err
	}

	token, err := generateToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate token: %w", err)
	}

	now := time.Now().UTC()
	player := &Player{
		ID:        s.nextPlayerID.Add(1) - 1,
		Nickname:  normalized,
		CreatedAt: now,
	}
	expiresAt := now.Add(s.tokenTTL)
	session := &Session{
		Token:     token,
		PlayerID:  player.ID,
		Nickname:  player.Nickname,
		ExpiresAt: expiresAt,
	}

	s.mu.Lock()
	s.players[player.ID] = player
	s.sessions[token] = session
	s.mu.Unlock()

	return LoginResult{
		Player:    *player,
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

// Authenticate 校验 token，成功返回对应会话；过期 token 会被清理。
func (s *Service) Authenticate(token string) (Session, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Session{}, ErrTokenNotFound
	}

	s.mu.RLock()
	session, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return Session{}, ErrTokenNotFound
	}

	if time.Now().UTC().After(session.ExpiresAt) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return Session{}, ErrTokenExpired
	}

	return *session, nil
}

// normalizeNickname 清洗并校验昵称。
func (s *Service) normalizeNickname(nickname string) (string, error) {
	normalized := strings.TrimSpace(nickname)
	if normalized == "" {
		return "游客", nil
	}
	if utf8.RuneCountInString(normalized) > s.maxNicknameLen {
		return "", fmt.Errorf("%w: 长度不能超过 %d 个字符", ErrInvalidNickname, s.maxNicknameLen)
	}
	// 拒绝控制字符，避免日志与展示异常。
	for _, runeValue := range normalized {
		if runeValue < 32 || runeValue == 127 {
			return "", fmt.Errorf("%w: 包含非法字符", ErrInvalidNickname)
		}
	}
	return normalized, nil
}

// generateToken 生成加密安全的随机 token（32 字节 → 64 位 hex）。
func generateToken() (string, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
