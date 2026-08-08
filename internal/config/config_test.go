package config

import (
	"testing"
	"time"
)

// TestLoadLifecycleConfig 验证回合计时、空房回收与指标参数可以从环境变量加载。
func TestLoadLifecycleConfig(t *testing.T) {
	t.Setenv("UNO_TURN_TIMEOUT", "17s")
	t.Setenv("UNO_MANAGED_ACTION_DELAY", "350ms")
	t.Setenv("UNO_TIMEOUT_STRIKE_LIMIT", "3")
	t.Setenv("UNO_EMPTY_ROOM_TTL", "45s")
	t.Setenv("UNO_METRICS_ENABLED", "false")

	loaded := Load()
	if loaded.TurnTimeout != 17*time.Second {
		t.Fatalf("TurnTimeout=%s", loaded.TurnTimeout)
	}
	if loaded.ManagedActionDelay != 350*time.Millisecond {
		t.Fatalf("ManagedActionDelay=%s", loaded.ManagedActionDelay)
	}
	if loaded.TimeoutStrikeLimit != 3 {
		t.Fatalf("TimeoutStrikeLimit=%d", loaded.TimeoutStrikeLimit)
	}
	if loaded.EmptyRoomTTL != 45*time.Second || loaded.MetricsEnabled {
		t.Fatalf("空房时限或指标开关加载失败：ttl=%s metrics=%v", loaded.EmptyRoomTTL, loaded.MetricsEnabled)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("合法配置校验失败：%v", err)
	}
}

// TestValidateRejectsInvalidLifecycleConfig 验证非正数生命周期参数会阻止服务启动。
func TestValidateRejectsInvalidLifecycleConfig(t *testing.T) {
	valid := Config{
		HTTPAddr:           ":8080",
		ShutdownTimeout:    time.Second,
		TokenTTL:           time.Hour,
		MaxNicknameLen:     32,
		TurnTimeout:        20 * time.Second,
		ManagedActionDelay: 200 * time.Millisecond,
		TimeoutStrikeLimit: 2,
		EmptyRoomTTL:       time.Minute,
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "回合超时", mutate: func(config *Config) { config.TurnTimeout = 0 }},
		{name: "托管间隔", mutate: func(config *Config) { config.ManagedActionDelay = 0 }},
		{name: "连续超时次数", mutate: func(config *Config) { config.TimeoutStrikeLimit = 0 }},
		{name: "空房保留时限", mutate: func(config *Config) { config.EmptyRoomTTL = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("非法配置未被拒绝")
			}
		})
	}
}

// TestLoadMySQLConfig 验证 M5 节点号、连接池和迁移参数可以从环境变量加载。
func TestLoadMySQLConfig(t *testing.T) {
	t.Setenv("UNO_NODE_ID", "23")
	t.Setenv("UNO_MYSQL_DSN", "uno:secret@tcp(127.0.0.1:3306)/uno")
	t.Setenv("UNO_MYSQL_MAX_OPEN_CONNS", "20")
	t.Setenv("UNO_MYSQL_MAX_IDLE_CONNS", "8")
	t.Setenv("UNO_MYSQL_CONN_MAX_LIFETIME", "4m")
	t.Setenv("UNO_MYSQL_CONN_MAX_IDLE_TIME", "45s")
	t.Setenv("UNO_MYSQL_OPERATION_TIMEOUT", "1500ms")
	t.Setenv("UNO_MYSQL_AUTO_MIGRATE", "false")

	loaded := Load()
	if loaded.NodeID != 23 || loaded.MySQLDSN == "" {
		t.Fatalf("节点或 DSN 加载失败：node=%d dsn=%q", loaded.NodeID, loaded.MySQLDSN)
	}
	if loaded.MySQLMaxOpenConns != 20 || loaded.MySQLMaxIdleConns != 8 {
		t.Fatalf("连接池参数不正确：open=%d idle=%d", loaded.MySQLMaxOpenConns, loaded.MySQLMaxIdleConns)
	}
	if loaded.MySQLConnMaxLifetime != 4*time.Minute || loaded.MySQLConnMaxIdleTime != 45*time.Second {
		t.Fatalf(
			"连接时限不正确：lifetime=%s idle=%s",
			loaded.MySQLConnMaxLifetime,
			loaded.MySQLConnMaxIdleTime,
		)
	}
	if loaded.MySQLOperationTimeout != 1500*time.Millisecond || loaded.MySQLAutoMigrate {
		t.Fatalf(
			"操作超时或迁移开关不正确：timeout=%s migrate=%v",
			loaded.MySQLOperationTimeout,
			loaded.MySQLAutoMigrate,
		)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("合法 MySQL 配置校验失败：%v", err)
	}
}

// TestValidateRejectsInvalidMySQLConfig 验证启用 MySQL 后会严格校验节点与连接池参数。
func TestValidateRejectsInvalidMySQLConfig(t *testing.T) {
	valid := Config{
		HTTPAddr:              ":8080",
		ShutdownTimeout:       time.Second,
		TokenTTL:              time.Hour,
		MaxNicknameLen:        32,
		TurnTimeout:           20 * time.Second,
		ManagedActionDelay:    200 * time.Millisecond,
		TimeoutStrikeLimit:    2,
		EmptyRoomTTL:          time.Minute,
		NodeID:                1,
		MySQLDSN:              "uno:secret@tcp(127.0.0.1:3306)/uno",
		MySQLMaxOpenConns:     10,
		MySQLMaxIdleConns:     5,
		MySQLConnMaxLifetime:  3 * time.Minute,
		MySQLConnMaxIdleTime:  time.Minute,
		MySQLOperationTimeout: 3 * time.Second,
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "节点号", mutate: func(config *Config) { config.NodeID = 1024 }},
		{name: "最大连接", mutate: func(config *Config) { config.MySQLMaxOpenConns = 0 }},
		{name: "零空闲连接", mutate: func(config *Config) { config.MySQLMaxIdleConns = 0 }},
		{name: "空闲连接", mutate: func(config *Config) { config.MySQLMaxIdleConns = config.MySQLMaxOpenConns + 1 }},
		{name: "连接寿命", mutate: func(config *Config) { config.MySQLConnMaxLifetime = 0 }},
		{name: "空闲时限", mutate: func(config *Config) { config.MySQLConnMaxIdleTime = 0 }},
		{name: "操作超时", mutate: func(config *Config) { config.MySQLOperationTimeout = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("非法 MySQL 配置未被拒绝")
			}
		})
	}
}

// TestLoadRedisConfig 验证 M6 Redis 连接池、超时与快照 TTL 可以从环境变量加载。
func TestLoadRedisConfig(t *testing.T) {
	t.Setenv("UNO_REDIS_ADDR", "127.0.0.1:6380")
	t.Setenv("UNO_REDIS_USERNAME", "uno")
	t.Setenv("UNO_REDIS_PASSWORD", "secret")
	t.Setenv("UNO_REDIS_DB", "3")
	t.Setenv("UNO_REDIS_POOL_SIZE", "18")
	t.Setenv("UNO_REDIS_MIN_IDLE_CONNS", "4")
	t.Setenv("UNO_REDIS_DIAL_TIMEOUT", "1800ms")
	t.Setenv("UNO_REDIS_READ_TIMEOUT", "900ms")
	t.Setenv("UNO_REDIS_WRITE_TIMEOUT", "1100ms")
	t.Setenv("UNO_REDIS_OPERATION_TIMEOUT", "1500ms")
	t.Setenv("UNO_REDIS_ROOM_SNAPSHOT_TTL", "90m")

	loaded := Load()
	if loaded.RedisAddr != "127.0.0.1:6380" || loaded.RedisUsername != "uno" ||
		loaded.RedisPassword != "secret" || loaded.RedisDB != 3 {
		t.Fatalf("Redis 连接参数加载失败：%+v", loaded)
	}
	if loaded.RedisPoolSize != 18 || loaded.RedisMinIdleConns != 4 {
		t.Fatalf("Redis 连接池参数错误：pool=%d idle=%d", loaded.RedisPoolSize, loaded.RedisMinIdleConns)
	}
	if loaded.RedisDialTimeout != 1800*time.Millisecond ||
		loaded.RedisReadTimeout != 900*time.Millisecond ||
		loaded.RedisWriteTimeout != 1100*time.Millisecond ||
		loaded.RedisOperationTimeout != 1500*time.Millisecond ||
		loaded.RedisRoomSnapshotTTL != 90*time.Minute {
		t.Fatalf("Redis 时限参数错误：%+v", loaded)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("合法 Redis 配置校验失败：%v", err)
	}
}

// TestValidateRejectsInvalidRedisConfig 验证启用 Redis 后会严格校验连接池和时限参数。
func TestValidateRejectsInvalidRedisConfig(t *testing.T) {
	valid := Config{
		HTTPAddr:              ":8080",
		ShutdownTimeout:       time.Second,
		TokenTTL:              time.Hour,
		MaxNicknameLen:        32,
		TurnTimeout:           20 * time.Second,
		ManagedActionDelay:    200 * time.Millisecond,
		TimeoutStrikeLimit:    2,
		EmptyRoomTTL:          time.Minute,
		NodeID:                1,
		RedisAddr:             "127.0.0.1:6379",
		RedisPoolSize:         10,
		RedisMinIdleConns:     1,
		RedisDialTimeout:      3 * time.Second,
		RedisReadTimeout:      2 * time.Second,
		RedisWriteTimeout:     2 * time.Second,
		RedisOperationTimeout: 2 * time.Second,
		RedisRoomSnapshotTTL:  2 * time.Hour,
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "数据库编号", mutate: func(config *Config) { config.RedisDB = -1 }},
		{name: "连接池", mutate: func(config *Config) { config.RedisPoolSize = 0 }},
		{name: "空闲连接", mutate: func(config *Config) { config.RedisMinIdleConns = 11 }},
		{name: "拨号超时", mutate: func(config *Config) { config.RedisDialTimeout = 0 }},
		{name: "读取超时", mutate: func(config *Config) { config.RedisReadTimeout = 0 }},
		{name: "写入超时", mutate: func(config *Config) { config.RedisWriteTimeout = 0 }},
		{name: "操作超时", mutate: func(config *Config) { config.RedisOperationTimeout = 0 }},
		{name: "快照 TTL", mutate: func(config *Config) { config.RedisRoomSnapshotTTL = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatal("非法 Redis 配置未被拒绝")
			}
		})
	}
}
