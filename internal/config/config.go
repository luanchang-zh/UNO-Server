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
