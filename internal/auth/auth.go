// Package auth 提供游客登录、玩家持久化、token 签发与跨进程校验。
//
// 玩家实体见 model/entity；哨兵错误见 model/errs；Redis 可作为 token 会话的跨进程真相源。
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/luanchang-zh/UNO-Server/internal/idgen"
	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/model/rediskey"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// TokenSession 表示一次登录会话，可同时存在于进程热缓存与 Redis。
type TokenSession struct {
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
	// Player 是本次创建并已经持久化的玩家实体。
	Player entity.Player
	// Token 是绑定该玩家的访问凭证。
	Token string
	// ExpiresAt 是 token 的 UTC 过期时间。
	ExpiresAt time.Time
}

// Options 控制鉴权服务行为。
type Options struct {
	// TokenTTL 为新签发 token 的有效期。
	TokenTTL time.Duration
	// MaxNicknameLen 为昵称最大 Unicode 字符数。
	MaxNicknameLen int
	// IDGenerator 为玩家生成跨进程可持久化的业务主键。
	IDGenerator idgen.Source
	// PlayerRepository 非空时要求登录成功前先持久化玩家。
	PlayerRepository store.PlayerRepository
	// PersistenceTimeout 限制单次玩家持久化的最长时间。
	PersistenceTimeout time.Duration
	// SessionRepository 非空时把 token 会话写入 Redis，并在内存未命中时回源。
	SessionRepository store.SessionRepository
	// SessionTimeout 限制单次 Redis 会话读写的最长时间。
	SessionTimeout time.Duration
}

// Service 提供登录与 token 校验能力。
type Service struct {
	tokenTTL       time.Duration
	maxNicknameLen int
	idGenerator    idgen.Source
	playersStore   store.PlayerRepository
	persistTimeout time.Duration
	sessionsStore  store.SessionRepository
	sessionTimeout time.Duration

	// sessions 是本进程热缓存；启用 Redis 时，缓存未命中会回源读取。
	mu       sync.RWMutex
	players  map[int64]*entity.Player
	sessions map[string]*TokenSession
}

// NewService 创建鉴权服务；玩家与 token 仓储均可选，未注入时保持纯内存模式。
func NewService(options Options) *Service {
	if options.TokenTTL <= 0 {
		options.TokenTTL = rediskey.DefaultSessionTTL
	}
	if options.MaxNicknameLen <= 0 {
		options.MaxNicknameLen = 32
	}
	if options.IDGenerator == nil {
		options.IDGenerator = defaultIDGenerator()
	}
	if options.PersistenceTimeout <= 0 {
		options.PersistenceTimeout = 3 * time.Second
	}
	if options.SessionTimeout <= 0 {
		options.SessionTimeout = 2 * time.Second
	}

	service := &Service{
		tokenTTL:       options.TokenTTL,
		maxNicknameLen: options.MaxNicknameLen,
		idGenerator:    options.IDGenerator,
		playersStore:   options.PlayerRepository,
		persistTimeout: options.PersistenceTimeout,
		sessionsStore:  options.SessionRepository,
		sessionTimeout: options.SessionTimeout,
		players:        make(map[int64]*entity.Player),
		sessions:       make(map[string]*TokenSession),
	}
	return service
}

// LoginGuest 使用后台上下文创建游客玩家，主要供内部调用和测试使用。
func (s *Service) LoginGuest(nickname string) (LoginResult, error) {
	return s.LoginGuestContext(context.Background(), nickname)
}

// LoginGuestContext 创建游客玩家并签发 token；启用仓储时依次写 MySQL、Redis，再发布内存会话。
func (s *Service) LoginGuestContext(ctx context.Context, nickname string) (LoginResult, error) {
	normalized, err := s.normalizeNickname(nickname)
	if err != nil {
		return LoginResult{}, err
	}

	token, err := generateToken()
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate token: %w", err)
	}
	playerID, err := s.idGenerator.Next()
	if err != nil {
		return LoginResult{}, fmt.Errorf("generate player id: %w", err)
	}

	now := time.Now().UTC()
	player := &entity.Player{
		ID:          playerID,
		Nickname:    normalized,
		CreatedAt:   now,
		UpdatedAt:   now,
		LastLoginAt: now,
	}
	expiresAt := now.Add(s.tokenTTL)
	session := &TokenSession{
		Token:     token,
		PlayerID:  player.ID,
		Nickname:  player.Nickname,
		ExpiresAt: expiresAt,
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.playersStore != nil {
		persistCtx, cancel := context.WithTimeout(ctx, s.persistTimeout)
		err = s.playersStore.CreatePlayer(persistCtx, *player)
		cancel()
		if err != nil {
			return LoginResult{}, fmt.Errorf("persist player %d: %w", player.ID, err)
		}
	}
	if s.sessionsStore != nil {
		sessionTTL := time.Until(session.ExpiresAt)
		if sessionTTL <= 0 {
			return LoginResult{}, errs.ErrTokenExpired
		}
		sessionCtx, cancel := context.WithTimeout(ctx, s.sessionTimeout)
		err = s.sessionsStore.SaveSession(sessionCtx, token, store.SessionRecord{
			PlayerID:  session.PlayerID,
			Nickname:  session.Nickname,
			ExpiresAt: session.ExpiresAt,
		}, sessionTTL)
		cancel()
		if err != nil {
			return LoginResult{}, fmt.Errorf("persist token session for player %d: %w", player.ID, err)
		}
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

// defaultIDGenerator 创建单进程默认节点生成器；生产入口会注入显式节点号。
func defaultIDGenerator() idgen.Source {
	generator, err := idgen.New(0)
	if err != nil {
		panic(fmt.Sprintf("create default id generator: %v", err))
	}
	return generator
}

// Authenticate 使用后台上下文校验 token，主要供内部调用和测试使用。
func (s *Service) Authenticate(token string) (TokenSession, error) {
	return s.AuthenticateContext(context.Background(), token)
}

// AuthenticateContext 优先读取内存热缓存，未命中时回源 Redis 并重新填充缓存。
func (s *Service) AuthenticateContext(ctx context.Context, token string) (TokenSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return TokenSession{}, errs.ErrTokenNotFound
	}
	if session, found := s.memorySession(token); found {
		return s.validateSession(ctx, token, session)
	}
	if s.sessionsStore == nil {
		return TokenSession{}, errs.ErrTokenNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	readCtx, cancel := context.WithTimeout(ctx, s.sessionTimeout)
	record, err := s.sessionsStore.FindSession(readCtx, token)
	cancel()
	if errors.Is(err, errs.ErrNotFound) {
		return TokenSession{}, errs.ErrTokenNotFound
	}
	if err != nil {
		return TokenSession{}, fmt.Errorf("load token session: %w", err)
	}
	session := TokenSession{
		Token:     token,
		PlayerID:  record.PlayerID,
		Nickname:  record.Nickname,
		ExpiresAt: record.ExpiresAt.UTC(),
	}
	validated, err := s.validateSession(ctx, token, session)
	if err != nil {
		return TokenSession{}, err
	}
	s.mu.Lock()
	s.sessions[token] = &session
	s.mu.Unlock()
	return validated, nil
}

// memorySession 只读取本进程 token 热缓存，不触发外部 IO。
func (s *Service) memorySession(token string) (TokenSession, bool) {
	s.mu.RLock()
	session, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok || session == nil {
		return TokenSession{}, false
	}
	return *session, true
}

// validateSession 检查绝对过期时间，并清理已经失效的内存与 Redis 会话。
func (s *Service) validateSession(ctx context.Context, token string, session TokenSession) (TokenSession, error) {
	if !time.Now().UTC().Before(session.ExpiresAt) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		if s.sessionsStore != nil {
			if ctx == nil {
				ctx = context.Background()
			}
			deleteCtx, cancel := context.WithTimeout(ctx, s.sessionTimeout)
			_ = s.sessionsStore.DeleteSession(deleteCtx, token)
			cancel()
		}
		return TokenSession{}, errs.ErrTokenExpired
	}
	return session, nil
}

// normalizeNickname 清洗并校验昵称。
func (s *Service) normalizeNickname(nickname string) (string, error) {
	normalized := strings.TrimSpace(nickname)
	if normalized == "" {
		return "游客", nil
	}
	if utf8.RuneCountInString(normalized) > s.maxNicknameLen {
		return "", fmt.Errorf("%w: 长度不能超过 %d 个字符", errs.ErrInvalidNickname, s.maxNicknameLen)
	}
	// 拒绝控制字符，避免日志与展示异常。
	for _, runeValue := range normalized {
		if runeValue < 32 || runeValue == 127 {
			return "", fmt.Errorf("%w: 包含非法字符", errs.ErrInvalidNickname)
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
