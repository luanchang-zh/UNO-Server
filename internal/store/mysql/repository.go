// Package mysql 提供基于 database/sql 的 MySQL 持久化实现。
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/luanchang-zh/UNO-Server/internal/store"
)

const (
	defaultMaxOpenConns    = 10
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 3 * time.Minute
	defaultConnMaxIdleTime = time.Minute
)

// Options 控制 MySQL 连接参数和连接池上限。
type Options struct {
	// DSN 是 go-sql-driver/mysql 格式的连接字符串。
	DSN string
	// MaxOpenConns 是连接池允许的最大打开连接数。
	MaxOpenConns int
	// MaxIdleConns 是连接池保留的最大空闲连接数。
	MaxIdleConns int
	// ConnMaxLifetime 是单连接允许复用的最长时间。
	ConnMaxLifetime time.Duration
	// ConnMaxIdleTime 是空闲连接允许保留的最长时间。
	ConnMaxIdleTime time.Duration
}

// Repository 复用一个 sql.DB 连接池实现全部 MySQL 数据访问。
type Repository struct {
	db *sql.DB
}

var (
	_ store.PlayerRepository = (*Repository)(nil)
	_ store.MatchRepository  = (*Repository)(nil)
)

// Open 规范化 DSN、建立连接池并在返回前验证数据库可达。
func Open(ctx context.Context, options Options) (*Repository, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	dsn, err := normalizeDSN(options.DSN)
	if err != nil {
		return nil, err
	}
	options = normalizeOptions(options)
	if options.MaxIdleConns > options.MaxOpenConns {
		return nil, fmt.Errorf("mysql max idle connections exceed max open connections")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	db.SetMaxOpenConns(options.MaxOpenConns)
	db.SetMaxIdleConns(options.MaxIdleConns)
	db.SetConnMaxLifetime(options.ConnMaxLifetime)
	db.SetConnMaxIdleTime(options.ConnMaxIdleTime)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	return &Repository{db: db}, nil
}

// New 使用已有 sql.DB 创建 Repository，主要供测试和受管运行环境注入连接池。
func New(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("mysql db is nil")
	}
	return &Repository{db: db}, nil
}

// Close 关闭底层连接池；正在使用的连接会由 database/sql 安全回收。
func (r *Repository) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

// normalizeOptions 为未设置的连接池参数填充保守默认值。
func normalizeOptions(options Options) Options {
	if options.MaxOpenConns <= 0 {
		options.MaxOpenConns = defaultMaxOpenConns
	}
	if options.MaxIdleConns <= 0 {
		options.MaxIdleConns = defaultMaxIdleConns
	}
	if options.ConnMaxLifetime <= 0 {
		options.ConnMaxLifetime = defaultConnMaxLifetime
	}
	if options.ConnMaxIdleTime <= 0 {
		options.ConnMaxIdleTime = defaultConnMaxIdleTime
	}
	return options
}

// normalizeDSN 强制开启时间解析、UTC 会话时区和 utf8mb4 连接排序规则。
func normalizeDSN(rawDSN string) (string, error) {
	if strings.TrimSpace(rawDSN) == "" {
		return "", fmt.Errorf("mysql dsn is empty")
	}
	driverConfig, err := mysqldriver.ParseDSN(rawDSN)
	if err != nil {
		return "", fmt.Errorf("parse mysql dsn: %w", err)
	}
	driverConfig.ParseTime = true
	driverConfig.Loc = time.UTC
	driverConfig.Collation = "utf8mb4_unicode_ci"
	if driverConfig.Params == nil {
		driverConfig.Params = make(map[string]string)
	}
	driverConfig.Params["time_zone"] = "'+00:00'"
	return driverConfig.FormatDSN(), nil
}
