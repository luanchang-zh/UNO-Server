package idgen

import (
	"sync"
	"testing"
	"time"
)

// TestGeneratorConcurrentUnique 验证并发生成的 ID 均为正数且不重复。
func TestGeneratorConcurrentUnique(t *testing.T) {
	generator, err := New(17)
	if err != nil {
		t.Fatalf("创建生成器失败：%v", err)
	}

	const workerCount = 8
	const idsPerWorker = 1000
	ids := make(chan int64, workerCount*idsPerWorker)
	errors := make(chan error, workerCount)
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := 0; index < idsPerWorker; index++ {
				id, generateErr := generator.Next()
				if generateErr != nil {
					errors <- generateErr
					return
				}
				ids <- id
			}
		}()
	}
	waitGroup.Wait()
	close(ids)
	close(errors)
	for generateErr := range errors {
		t.Fatalf("并发生成失败：%v", generateErr)
	}

	seen := make(map[int64]struct{}, workerCount*idsPerWorker)
	for id := range ids {
		if id <= 0 {
			t.Fatalf("生成了非正数 ID：%d", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("生成了重复 ID：%d", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != workerCount*idsPerWorker {
		t.Fatalf("ID 数量=%d，期望=%d", len(seen), workerCount*idsPerWorker)
	}
}

// TestGeneratorEncodesNodeAndSequence 验证同毫秒序列递增且节点号写入预期位段。
func TestGeneratorEncodesNodeAndSequence(t *testing.T) {
	fixedTime := time.UnixMilli(customEpochMillis + 1234)
	generator, err := newGenerator(9, func() time.Time { return fixedTime })
	if err != nil {
		t.Fatalf("创建生成器失败：%v", err)
	}
	first, err := generator.Next()
	if err != nil {
		t.Fatalf("生成首个 ID 失败：%v", err)
	}
	second, err := generator.Next()
	if err != nil {
		t.Fatalf("生成第二个 ID 失败：%v", err)
	}
	if second != first+1 {
		t.Fatalf("同毫秒序列未递增：first=%d second=%d", first, second)
	}
	if nodeID := first >> nodeShift & maxNodeID; nodeID != 9 {
		t.Fatalf("节点位段=%d，期望=9", nodeID)
	}
}

// TestGeneratorRejectsClockRollback 验证时钟回拨时拒绝生成可能重复的 ID。
func TestGeneratorRejectsClockRollback(t *testing.T) {
	times := []time.Time{
		time.UnixMilli(customEpochMillis + 10),
		time.UnixMilli(customEpochMillis + 9),
	}
	index := 0
	generator, err := newGenerator(1, func() time.Time {
		value := times[index]
		if index < len(times)-1 {
			index++
		}
		return value
	})
	if err != nil {
		t.Fatalf("创建生成器失败：%v", err)
	}
	if _, err := generator.Next(); err != nil {
		t.Fatalf("首次生成失败：%v", err)
	}
	if _, err := generator.Next(); err == nil {
		t.Fatal("时钟回拨未被拒绝")
	}
}

// TestNewRejectsInvalidNode 验证越界节点号不能启动生成器。
func TestNewRejectsInvalidNode(t *testing.T) {
	if _, err := New(-1); err == nil {
		t.Fatal("负节点号未被拒绝")
	}
	if _, err := New(maxNodeID + 1); err == nil {
		t.Fatal("超大节点号未被拒绝")
	}
}
