// Package redis 提供基于 go-redis 的 token 会话与房间快照持久化实现。
package redis

import (
	"context"
	"fmt"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/luanchang-zh/UNO-Server/internal/store"
)

const (
	defaultPoolSize     = 10
	defaultDialTimeout  = 3 * time.Second
	defaultReadTimeout  = 2 * time.Second
	defaultWriteTimeout = 2 * time.Second
)

// Options 控制 Redis 单节点连接与连接池参数。
type Options struct {
	// Addr 是 Redis 的 host:port 地址。
	Addr string
	// Username 是 Redis ACL 用户名；未启用 ACL 时留空。
	Username string
	// Password 是 Redis ACL 或 requirepass 密码。
	Password string
	// DB 是单节点 Redis 的逻辑数据库编号。
	DB int
	// PoolSize 是连接池允许保留的最大连接数。
	PoolSize int
	// MinIdleConns 是连接池预留的最小空闲连接数。
	MinIdleConns int
	// DialTimeout 是建立新连接的最长等待时间。
	DialTimeout time.Duration
	// ReadTimeout 是单条命令读取响应的最长等待时间。
	ReadTimeout time.Duration
	// WriteTimeout 是单条命令写入请求的最长等待时间。
	WriteTimeout time.Duration
}

// Repository 复用一个并发安全的 Redis 客户端实现会话与快照端口。
type Repository struct {
	client goredis.UniversalClient
}

var (
	_ store.SessionRepository      = (*Repository)(nil)
	_ store.RoomSnapshotRepository = (*Repository)(nil)
)

// Open 建立 Redis 连接池，并在返回前使用调用方上下文验证服务可达。
func Open(ctx context.Context, options Options) (*Repository, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	normalized, err := normalizeOptions(options)
	if err != nil {
		return nil, err
	}
	client := goredis.NewClient(&goredis.Options{
		Addr:         normalized.Addr,
		Username:     normalized.Username,
		Password:     normalized.Password,
		DB:           normalized.DB,
		PoolSize:     normalized.PoolSize,
		MinIdleConns: normalized.MinIdleConns,
		DialTimeout:  normalized.DialTimeout,
		ReadTimeout:  normalized.ReadTimeout,
		WriteTimeout: normalized.WriteTimeout,
		ClientName:   "uno-server",
	})
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &Repository{client: client}, nil
}

// New 使用受管 Redis 客户端创建 Repository，主要供测试或托管运行环境注入。
func New(client goredis.UniversalClient) (*Repository, error) {
	if client == nil {
		return nil, fmt.Errorf("redis client 不能为空")
	}
	return &Repository{client: client}, nil
}

// Close 释放 Redis 连接池；可由进程退出路径安全调用。
func (r *Repository) Close() error {
	if r == nil || r.client == nil {
		return nil
	}
	return r.client.Close()
}

// normalizeOptions 填充连接池默认值，并拒绝会被客户端静默误解的参数。
func normalizeOptions(options Options) (Options, error) {
	options.Addr = strings.TrimSpace(options.Addr)
	if options.Addr == "" {
		return Options{}, fmt.Errorf("redis addr 不能为空")
	}
	if options.DB < 0 {
		return Options{}, fmt.Errorf("redis db 不能小于 0")
	}
	if options.PoolSize == 0 {
		options.PoolSize = defaultPoolSize
	}
	if options.PoolSize < 0 {
		return Options{}, fmt.Errorf("redis pool size 不能小于 0")
	}
	if options.MinIdleConns < 0 || options.MinIdleConns > options.PoolSize {
		return Options{}, fmt.Errorf("redis min idle conns 必须在 0–pool size 范围内")
	}
	if options.DialTimeout == 0 {
		options.DialTimeout = defaultDialTimeout
	}
	if options.ReadTimeout == 0 {
		options.ReadTimeout = defaultReadTimeout
	}
	if options.WriteTimeout == 0 {
		options.WriteTimeout = defaultWriteTimeout
	}
	if options.DialTimeout < 0 || options.ReadTimeout < 0 || options.WriteTimeout < 0 {
		return Options{}, fmt.Errorf("redis 连接与读写超时不能小于 0")
	}
	return options, nil
}
