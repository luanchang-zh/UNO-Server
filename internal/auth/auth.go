// Package auth 提供游客登录、玩家持久化、token 签发与内存校验。
//
// 玩家实体见 model/entity；哨兵错误见 model/errs；后续 token 可落到 Redis（rediskey.Session）。
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/luanchang-zh/UNO-Server/internal/idgen"
	"github.com/luanchang-zh/UNO-Server/internal/model/entity"
	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// TokenSession 表示一次登录会话（token 与玩家绑定，非 MySQL 表；后续对应 Redis session）。
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
	// Token 是绑定该玩家的内存访问凭证。
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
}

// Service 提供登录与 token 校验能力。
type Service struct {
	tokenTTL       time.Duration
	maxNicknameLen int
	idGenerator    idgen.Source
	playersStore   store.PlayerRepository
	persistTimeout time.Duration

	mu       sync.RWMutex
	players  map[int64]*entity.Player
	sessions map[string]*TokenSession // token 到会话的索引
}

// NewService 创建鉴权服务；玩家仓储可选，token 会话保持进程内存实现。
func NewService(options Options) *Service {
	if options.TokenTTL <= 0 {
		options.TokenTTL = 24 * time.Hour
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

	service := &Service{
		tokenTTL:       options.TokenTTL,
		maxNicknameLen: options.MaxNicknameLen,
		idGenerator:    options.IDGenerator,
		playersStore:   options.PlayerRepository,
		persistTimeout: options.PersistenceTimeout,
		players:        make(map[int64]*entity.Player),
		sessions:       make(map[string]*TokenSession),
	}
	return service
}

// LoginGuest 使用后台上下文创建游客玩家，主要供内部调用和测试使用。
func (s *Service) LoginGuest(nickname string) (LoginResult, error) {
	return s.LoginGuestContext(context.Background(), nickname)
}

// LoginGuestContext 创建游客玩家并签发 token；启用仓储时先写 MySQL 再发布内存会话。
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
	if s.playersStore != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		persistCtx, cancel := context.WithTimeout(ctx, s.persistTimeout)
		err = s.playersStore.CreatePlayer(persistCtx, *player)
		cancel()
		if err != nil {
			return LoginResult{}, fmt.Errorf("persist player %d: %w", player.ID, err)
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

// Authenticate 校验 token，成功返回对应会话；过期 token 会被清理。
func (s *Service) Authenticate(token string) (TokenSession, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return TokenSession{}, errs.ErrTokenNotFound
	}

	s.mu.RLock()
	session, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return TokenSession{}, errs.ErrTokenNotFound
	}

	if time.Now().UTC().After(session.ExpiresAt) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return TokenSession{}, errs.ErrTokenExpired
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
