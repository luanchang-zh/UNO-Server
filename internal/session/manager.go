package session

import (
	"sync"

	"github.com/luanchang-zh/UNO-Server/internal/logx"
)

// Manager 登记所有活跃 WebSocket 会话，供查找与优雅关闭时统一断开。
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	logger   *logx.Logger
}

// NewManager 创建空的会话管理器。
func NewManager(logger *logx.Logger) *Manager {
	if logger == nil {
		logger = logx.NewFromSlog(nil)
	}
	return &Manager{
		sessions: make(map[string]*Session),
		logger:   logger,
	}
}

// Register 登记会话；同 ID 重复登记会覆盖（正常路径 ID 唯一）。
func (m *Manager) Register(playerSession *Session) {
	if m == nil || playerSession == nil {
		return
	}
	m.mu.Lock()
	m.sessions[playerSession.ID] = playerSession
	m.mu.Unlock()
}

// Unregister 移除会话登记；幂等。
func (m *Manager) Unregister(sessionID string) {
	if m == nil || sessionID == "" {
		return
	}
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
}

// Get 按连接 ID 查找会话。
func (m *Manager) Get(sessionID string) (*Session, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	playerSession, ok := m.sessions[sessionID]
	return playerSession, ok
}

// Count 返回当前登记的连接数。
func (m *Manager) Count() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// CloseAll 关闭全部已登记会话（用于进程优雅退出）。
func (m *Manager) CloseAll() {
	if m == nil {
		return
	}
	m.mu.RLock()
	snapshot := make([]*Session, 0, len(m.sessions))
	for _, playerSession := range m.sessions {
		snapshot = append(snapshot, playerSession)
	}
	m.mu.RUnlock()

	for _, playerSession := range snapshot {
		playerSession.Close()
	}
}
