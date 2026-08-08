// Package config 负责服务启动配置的加载与校验。
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config 汇总进程级运行参数。
type Config struct {
	// HTTPAddr 为 HTTP 监听地址，例如 ":8080"。
	HTTPAddr string
	// ReadTimeout 为单次 HTTP 请求读超时。
	ReadTimeout time.Duration
	// WriteTimeout 为单次 HTTP 请求写超时。
	WriteTimeout time.Duration
	// ShutdownTimeout 为优雅退出等待上限。
	ShutdownTimeout time.Duration
	// TokenTTL 为游客登录 token 有效期。
	TokenTTL time.Duration
	// MaxNicknameLen 为昵称最大字符数（按 Unicode 字符计）。
	MaxNicknameLen int
	// TurnTimeout 为当前玩家一次手动行动的最长等待时间。
	TurnTimeout time.Duration
	// ManagedActionDelay 为托管玩家连续自动行动之间的最小间隔。
	ManagedActionDelay time.Duration
	// TimeoutStrikeLimit 为玩家连续超时多少次后进入托管。
	TimeoutStrikeLimit int
	// EmptyRoomTTL 是所有成员断线后保留房间以等待重连的最长时间。
	EmptyRoomTTL time.Duration
	// MetricsEnabled 控制是否注册 Prometheus 指标端点。
	MetricsEnabled bool
	// NodeID 是雪花 ID 中的节点编号，范围为 0–1023。
	NodeID int64
	// MySQLDSN 为空时关闭 MySQL 持久化，非空时在启动阶段建立连接。
	MySQLDSN string
	// MySQLMaxOpenConns 是 MySQL 连接池最大打开连接数。
	MySQLMaxOpenConns int
	// MySQLMaxIdleConns 是 MySQL 连接池最大空闲连接数。
	MySQLMaxIdleConns int
	// MySQLConnMaxLifetime 是单个 MySQL 连接的最长复用时间。
	MySQLConnMaxLifetime time.Duration
	// MySQLConnMaxIdleTime 是 MySQL 空闲连接的最长保留时间。
	MySQLConnMaxIdleTime time.Duration
	// MySQLOperationTimeout 是启动、登录、开局与结算数据库操作的超时。
	MySQLOperationTimeout time.Duration
	// MySQLAutoMigrate 控制启动时是否幂等创建 M5 三张表。
	MySQLAutoMigrate bool
	// RedisAddr 为空时关闭 Redis，非空时启用 token 与房间快照持久化。
	RedisAddr string
	// RedisUsername 是 Redis ACL 用户名；未启用 ACL 时留空。
	RedisUsername string
	// RedisPassword 是 Redis ACL 或 requirepass 密码。
	RedisPassword string
	// RedisDB 是单节点 Redis 使用的逻辑数据库编号。
	RedisDB int
	// RedisPoolSize 是 Redis 连接池允许保留的最大连接数。
	RedisPoolSize int
	// RedisMinIdleConns 是 Redis 连接池预留的最小空闲连接数。
	RedisMinIdleConns int
	// RedisDialTimeout 是建立 Redis 连接的最长等待时间。
	RedisDialTimeout time.Duration
	// RedisReadTimeout 是读取 Redis 命令响应的最长等待时间。
	RedisReadTimeout time.Duration
	// RedisWriteTimeout 是写入 Redis 命令的最长等待时间。
	RedisWriteTimeout time.Duration
	// RedisOperationTimeout 是单次会话、快照或启动恢复操作的业务超时。
	RedisOperationTimeout time.Duration
	// RedisRoomSnapshotTTL 是活跃房间快照与玩家房间索引的兜底有效期。
	RedisRoomSnapshotTTL time.Duration
}

// Load 从环境变量读取配置，未设置时使用本地开发默认值。
func Load() Config {
	return Config{
		HTTPAddr:        envOrDefault("UNO_HTTP_ADDR", ":8080"),
		ReadTimeout:     envDurationOrDefault("UNO_READ_TIMEOUT", 5*time.Second),
		WriteTimeout:    envDurationOrDefault("UNO_WRITE_TIMEOUT", 5*time.Second),
		ShutdownTimeout: envDurationOrDefault("UNO_SHUTDOWN_TIMEOUT", 10*time.Second),
		TokenTTL:        envDurationOrDefault("UNO_TOKEN_TTL", 24*time.Hour),
		MaxNicknameLen:  envIntOrDefault("UNO_MAX_NICKNAME_LEN", 32),
		TurnTimeout:     envDurationOrDefault("UNO_TURN_TIMEOUT", 20*time.Second),
		ManagedActionDelay: envDurationOrDefault(
			"UNO_MANAGED_ACTION_DELAY",
			200*time.Millisecond,
		),
		TimeoutStrikeLimit: envIntOrDefault("UNO_TIMEOUT_STRIKE_LIMIT", 2),
		EmptyRoomTTL:       envDurationOrDefault("UNO_EMPTY_ROOM_TTL", 15*time.Minute),
		MetricsEnabled:     envBoolOrDefault("UNO_METRICS_ENABLED", true),
		NodeID:             int64(envIntOrDefault("UNO_NODE_ID", 1)),
		MySQLDSN:           envOrDefault("UNO_MYSQL_DSN", ""),
		MySQLMaxOpenConns:  envIntOrDefault("UNO_MYSQL_MAX_OPEN_CONNS", 10),
		MySQLMaxIdleConns:  envIntOrDefault("UNO_MYSQL_MAX_IDLE_CONNS", 5),
		MySQLConnMaxLifetime: envDurationOrDefault(
			"UNO_MYSQL_CONN_MAX_LIFETIME",
			3*time.Minute,
		),
		MySQLConnMaxIdleTime: envDurationOrDefault("UNO_MYSQL_CONN_MAX_IDLE_TIME", time.Minute),
		MySQLOperationTimeout: envDurationOrDefault(
			"UNO_MYSQL_OPERATION_TIMEOUT",
			3*time.Second,
		),
		MySQLAutoMigrate: envBoolOrDefault("UNO_MYSQL_AUTO_MIGRATE", true),
		RedisAddr:        envOrDefault("UNO_REDIS_ADDR", ""),
		RedisUsername:    envOrDefault("UNO_REDIS_USERNAME", ""),
		RedisPassword:    envOrDefault("UNO_REDIS_PASSWORD", ""),
		RedisDB:          envIntOrDefault("UNO_REDIS_DB", 0),
		RedisPoolSize:    envIntOrDefault("UNO_REDIS_POOL_SIZE", 10),
		RedisMinIdleConns: envIntOrDefault(
			"UNO_REDIS_MIN_IDLE_CONNS",
			1,
		),
		RedisDialTimeout:      envDurationOrDefault("UNO_REDIS_DIAL_TIMEOUT", 3*time.Second),
		RedisReadTimeout:      envDurationOrDefault("UNO_REDIS_READ_TIMEOUT", 2*time.Second),
		RedisWriteTimeout:     envDurationOrDefault("UNO_REDIS_WRITE_TIMEOUT", 2*time.Second),
		RedisOperationTimeout: envDurationOrDefault("UNO_REDIS_OPERATION_TIMEOUT", 2*time.Second),
		RedisRoomSnapshotTTL:  envDurationOrDefault("UNO_REDIS_ROOM_SNAPSHOT_TTL", 2*time.Hour),
	}
}

// Validate 检查关键配置，避免带着明显错误启动。
func (c Config) Validate() error {
	if c.HTTPAddr == "" {
		return fmt.Errorf("HTTPAddr 不能为空")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("ShutdownTimeout 必须大于 0")
	}
	if c.TokenTTL <= 0 {
		return fmt.Errorf("TokenTTL 必须大于 0")
	}
	if c.MaxNicknameLen <= 0 {
		return fmt.Errorf("MaxNicknameLen 必须大于 0")
	}
	if c.TurnTimeout <= 0 {
		return fmt.Errorf("TurnTimeout 必须大于 0")
	}
	if c.ManagedActionDelay <= 0 {
		return fmt.Errorf("ManagedActionDelay 必须大于 0")
	}
	if c.TimeoutStrikeLimit <= 0 {
		return fmt.Errorf("TimeoutStrikeLimit 必须大于 0")
	}
	if c.EmptyRoomTTL <= 0 {
		return fmt.Errorf("EmptyRoomTTL 必须大于 0")
	}
	if c.NodeID < 0 || c.NodeID > 1023 {
		return fmt.Errorf("NodeID 必须在 0–1023 范围内")
	}
	if c.MySQLDSN != "" {
		if c.MySQLMaxOpenConns <= 0 {
			return fmt.Errorf("MySQLMaxOpenConns 必须大于 0")
		}
		if c.MySQLMaxIdleConns <= 0 || c.MySQLMaxIdleConns > c.MySQLMaxOpenConns {
			return fmt.Errorf("MySQLMaxIdleConns 必须在 1–MySQLMaxOpenConns 范围内")
		}
		if c.MySQLConnMaxLifetime <= 0 {
			return fmt.Errorf("MySQLConnMaxLifetime 必须大于 0")
		}
		if c.MySQLConnMaxIdleTime <= 0 {
			return fmt.Errorf("MySQLConnMaxIdleTime 必须大于 0")
		}
		if c.MySQLOperationTimeout <= 0 {
			return fmt.Errorf("MySQLOperationTimeout 必须大于 0")
		}
	}
	if c.RedisAddr != "" {
		if c.RedisDB < 0 {
			return fmt.Errorf("RedisDB 不能小于 0")
		}
		if c.RedisPoolSize <= 0 {
			return fmt.Errorf("RedisPoolSize 必须大于 0")
		}
		if c.RedisMinIdleConns < 0 || c.RedisMinIdleConns > c.RedisPoolSize {
			return fmt.Errorf("RedisMinIdleConns 必须在 0–RedisPoolSize 范围内")
		}
		if c.RedisDialTimeout <= 0 || c.RedisReadTimeout <= 0 || c.RedisWriteTimeout <= 0 {
			return fmt.Errorf("Redis 连接与读写超时必须大于 0")
		}
		if c.RedisOperationTimeout <= 0 {
			return fmt.Errorf("RedisOperationTimeout 必须大于 0")
		}
		if c.RedisRoomSnapshotTTL <= 0 {
			return fmt.Errorf("RedisRoomSnapshotTTL 必须大于 0")
		}
	}
	return nil
}

// envOrDefault 读取字符串环境变量，空值时回落默认值。
func envOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// envDurationOrDefault 读取时长环境变量（支持 5s、1h 等格式）。
func envDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	if parsed, err := time.ParseDuration(value); err == nil {
		return parsed
	}
	return defaultValue
}

// envIntOrDefault 读取整数环境变量。
func envIntOrDefault(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}

// envBoolOrDefault 读取布尔环境变量，格式非法时回落默认值。
func envBoolOrDefault(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return parsed
}
