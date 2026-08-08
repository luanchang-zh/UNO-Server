// Package idgen 提供进程内线程安全的雪花 ID 生成能力。
package idgen

import (
	"fmt"
	"sync"
	"time"
)

const (
	sequenceBits   = uint(12)
	nodeBits       = uint(10)
	nodeShift      = sequenceBits
	timestampShift = sequenceBits + nodeBits
	maxSequence    = int64(1<<sequenceBits - 1)
	maxNodeID      = int64(1<<nodeBits - 1)
	maxTimestamp   = int64(1<<(63-timestampShift) - 1)
	// customEpochMillis 采用 2026-01-01 00:00:00 UTC，41 位毫秒时间可使用约 69 年。
	customEpochMillis = int64(1767225600000)
)

// Source 是需要生成业务主键的组件依赖的最小接口。
type Source interface {
	// Next 返回一个正数且在当前节点内单调递增的业务 ID。
	Next() (int64, error)
}

// Generator 使用 41 位毫秒时间、10 位节点号和 12 位序列号生成 ID。
type Generator struct {
	mu          sync.Mutex
	nodeID      int64
	lastMillis  int64
	sequence    int64
	now         func() time.Time
	waitBackoff time.Duration
}

// New 创建指定节点号的雪花 ID 生成器，节点号范围为 0–1023。
func New(nodeID int64) (*Generator, error) {
	return newGenerator(nodeID, time.Now)
}

// newGenerator 允许测试注入时钟，生产代码统一通过 New 创建。
func newGenerator(nodeID int64, now func() time.Time) (*Generator, error) {
	if nodeID < 0 || nodeID > maxNodeID {
		return nil, fmt.Errorf("node id %d is outside 0-%d", nodeID, maxNodeID)
	}
	if now == nil {
		return nil, fmt.Errorf("clock is nil")
	}
	return &Generator{
		nodeID:      nodeID,
		lastMillis:  -1,
		now:         now,
		waitBackoff: 100 * time.Microsecond,
	}, nil
}

// Next 在互斥区内推进毫秒序列，避免并发请求生成重复主键。
func (g *Generator) Next() (int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	elapsed, err := g.elapsedMillis()
	if err != nil {
		return 0, err
	}
	if elapsed < g.lastMillis {
		return 0, fmt.Errorf("clock moved backwards from %d to %d", g.lastMillis, elapsed)
	}
	if elapsed == g.lastMillis {
		g.sequence = (g.sequence + 1) & maxSequence
		if g.sequence == 0 {
			elapsed, err = g.waitNextMillis(g.lastMillis)
			if err != nil {
				return 0, err
			}
		}
	} else {
		g.sequence = 0
	}
	g.lastMillis = elapsed

	id := elapsed<<timestampShift | g.nodeID<<nodeShift | g.sequence
	if id <= 0 {
		return 0, fmt.Errorf("generated non-positive id %d", id)
	}
	return id, nil
}

// waitNextMillis 在单毫秒序列耗尽后等待时钟进入下一毫秒。
func (g *Generator) waitNextMillis(previous int64) (int64, error) {
	for {
		time.Sleep(g.waitBackoff)
		elapsed, err := g.elapsedMillis()
		if err != nil {
			return 0, err
		}
		if elapsed < previous {
			return 0, fmt.Errorf("clock moved backwards from %d to %d", previous, elapsed)
		}
		if elapsed > previous {
			return elapsed, nil
		}
	}
}

// elapsedMillis 返回自定义纪元以来的毫秒数，并检查 41 位时间范围。
func (g *Generator) elapsedMillis() (int64, error) {
	elapsed := g.now().UTC().UnixMilli() - customEpochMillis
	if elapsed < 0 || elapsed > maxTimestamp {
		return 0, fmt.Errorf("clock milliseconds %d exceed snowflake range", elapsed)
	}
	return elapsed, nil
}
