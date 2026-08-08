package session

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
)

// blockingCloseHook 模拟需要同步回投业务 actor 的关闭回调。
type blockingCloseHook struct {
	entered chan struct{}
	release chan struct{}
}

// OnSessionClose 记录回调已进入，并等待测试允许其返回。
func (h *blockingCloseHook) OnSessionClose(_ *Session) {
	close(h.entered)
	<-h.release
}

// TestSession_SendBufferFullClosesAsynchronously 验证背压关闭不会阻塞当前业务 actor。
func TestSession_SendBufferFullClosesAsynchronously(t *testing.T) {
	logger := logx.NewFromSlog(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	hook := &blockingCloseHook{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	playerSession := &Session{
		ID:          "slow-client",
		PlayerID:    1,
		send:        make(chan []byte, 1),
		logger:      logger,
		closeHook:   hook,
		connectedAt: time.Now(),
		closed:      make(chan struct{}),
	}
	playerSession.send <- []byte("占满队列")

	result := make(chan error, 1)
	go func() {
		result <- playerSession.Send([]byte("触发背压"))
	}()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("出站队列满时未返回错误")
		}
	case <-time.After(time.Second):
		close(hook.release)
		t.Fatal("发送被同步关闭回调阻塞")
	}

	select {
	case <-hook.entered:
	case <-time.After(time.Second):
		close(hook.release)
		t.Fatal("慢客户端未触发关闭回调")
	}
	close(hook.release)
}
