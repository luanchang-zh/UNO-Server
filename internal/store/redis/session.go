package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/luanchang-zh/UNO-Server/internal/model/errs"
	"github.com/luanchang-zh/UNO-Server/internal/model/rediskey"
	"github.com/luanchang-zh/UNO-Server/internal/store"
)

// SaveSession 将 token 会话序列化为 JSON，并以绝对过期时间对应的 TTL 保存。
func (r *Repository) SaveSession(
	ctx context.Context,
	token string,
	record store.SessionRecord,
	ttl time.Duration,
) error {
	if err := validateSession(token, record, ttl); err != nil {
		return err
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode redis session: %w", err)
	}
	if err := r.client.Set(contextOrBackground(ctx), rediskey.Session(token), payload, ttl).Err(); err != nil {
		return fmt.Errorf("save redis session: %w", err)
	}
	return nil
}

// FindSession 读取并校验 token 会话；键不存在时返回统一的资源不存在错误。
func (r *Repository) FindSession(ctx context.Context, token string) (store.SessionRecord, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return store.SessionRecord{}, fmt.Errorf("token 不能为空: %w", errs.ErrInvalidArgument)
	}
	payload, err := r.client.Get(contextOrBackground(ctx), rediskey.Session(token)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return store.SessionRecord{}, errs.ErrNotFound
	}
	if err != nil {
		return store.SessionRecord{}, fmt.Errorf("find redis session: %w", err)
	}
	var record store.SessionRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return store.SessionRecord{}, fmt.Errorf("decode redis session: %w", err)
	}
	if record.PlayerID <= 0 || strings.TrimSpace(record.Nickname) == "" || record.ExpiresAt.IsZero() {
		return store.SessionRecord{}, fmt.Errorf("redis session 内容非法: %w", errs.ErrInvalidArgument)
	}
	record.ExpiresAt = record.ExpiresAt.UTC()
	return record, nil
}

// DeleteSession 幂等删除 token 会话。
func (r *Repository) DeleteSession(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if err := r.client.Del(contextOrBackground(ctx), rediskey.Session(token)).Err(); err != nil {
		return fmt.Errorf("delete redis session: %w", err)
	}
	return nil
}

// validateSession 在写入前校验业务身份、过期时间和 Redis TTL。
func validateSession(token string, record store.SessionRecord, ttl time.Duration) error {
	if strings.TrimSpace(token) == "" || record.PlayerID <= 0 || strings.TrimSpace(record.Nickname) == "" {
		return fmt.Errorf("redis session 参数非法: %w", errs.ErrInvalidArgument)
	}
	if record.ExpiresAt.IsZero() || ttl <= 0 {
		return fmt.Errorf("redis session 过期参数非法: %w", errs.ErrInvalidArgument)
	}
	return nil
}

// contextOrBackground 让仓储公开方法对 nil 上下文保持防御性兼容。
func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
