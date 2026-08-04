package session

import (
	"io"
	"log/slog"
	"testing"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
)

// TestManager_RegisterUnregister 验证登记、注销与计数。
func TestManager_RegisterUnregister(t *testing.T) {
	logger := logx.NewFromSlog(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	manager := NewManager(logger)

	manager.Register(&Session{ID: "x1", logger: logger})
	manager.Register(&Session{ID: "x2", logger: logger})
	if manager.Count() != 2 {
		t.Fatalf("count=%d", manager.Count())
	}

	manager.Unregister("x1")
	if manager.Count() != 1 {
		t.Fatalf("count after unregister=%d", manager.Count())
	}

	manager.Unregister("x1") // 幂等
	if manager.Count() != 1 {
		t.Fatalf("count=%d", manager.Count())
	}

	got, ok := manager.Get("x2")
	if !ok || got.ID != "x2" {
		t.Fatalf("Get x2 failed")
	}
}
