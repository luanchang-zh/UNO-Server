package config

import (
	"testing"
	"time"
)

// TestLoadLifecycleConfig 验证 M4 回合计时与托管参数可以从环境变量加载。
func TestLoadLifecycleConfig(t *testing.T) {
	t.Setenv("UNO_TURN_TIMEOUT", "17s")
	t.Setenv("UNO_MANAGED_ACTION_DELAY", "350ms")
	t.Setenv("UNO_TIMEOUT_STRIKE_LIMIT", "3")

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
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "回合超时", mutate: func(config *Config) { config.TurnTimeout = 0 }},
		{name: "托管间隔", mutate: func(config *Config) { config.ManagedActionDelay = 0 }},
		{name: "连续超时次数", mutate: func(config *Config) { config.TimeoutStrikeLimit = 0 }},
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
