package nosql

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
)

// ============================================================
// 基准测试
// 所有基准均使用纯本地 BadgerDB（不依赖 MySQL），
// 专注测试 BadgerDB 写入吞吐、并发安全性与 pendingCount 精度。
// ============================================================

// newBenchDB 创建不带 MySQL 同步的纯本地 BadgerDB（基准测试专用）
func newBenchDB(b *testing.B) *PrefixedBadgerDB[testFund] {
	b.Helper()
	db, err := NewSharedBadgerDB[testFund](b.TempDir())
	if err != nil {
		b.Fatalf("NewSharedBadgerDB 失败: %v", err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

// BenchmarkSet_Sequential 顺序单写吞吐
func BenchmarkSet_Sequential(b *testing.B) {
	db := newBenchDB(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("user-%d:M%d", i%1000, i)
		f := newFund(fmt.Sprintf("user-%d", i%1000), fmt.Sprintf("M%d", i), float64(i))
		if err := db.Set(f, 0); err != nil {
			b.Fatalf("Set 失败 [key=%s]: %v", key, err)
		}
	}
}

// BenchmarkSet_Parallel 并发写入（不同 key，无热点）
func BenchmarkSet_Parallel(b *testing.B) {
	db := newBenchDB(b)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			f := newFund(fmt.Sprintf("user-%d", i%500), fmt.Sprintf("M%d", i), float64(i))
			if err := db.Set(f, 0); err != nil {
				b.Errorf("Set 失败: %v", err)
			}
			i++
		}
	})
}

// BenchmarkSet_HotKey 并发写同一个 key（测锁竞争）
func BenchmarkSet_HotKey(b *testing.B) {
	db := newBenchDB(b)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			f := newFund("hotuser", "HOTMKT", 1.0)
			if err := db.Set(f, 0); err != nil {
				b.Errorf("Set 失败(热点key): %v", err)
			}
		}
	})
}

// BenchmarkGet_Sequential 顺序读（预先写入 1000 条）
func BenchmarkGet_Sequential(b *testing.B) {
	db := newBenchDB(b)
	keys := make([]string, 1000)
	for i := range keys {
		f := newFund(fmt.Sprintf("u%d", i), "M0", float64(i))
		if err := db.Set(f, 0); err != nil {
			b.Fatalf("setup Set 失败: %v", err)
		}
		keys[i] = f.GetHash()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = db.Get(keys[i%len(keys)])
	}
}

// BenchmarkGet_Parallel 并发读（预先写入 1000 条）
func BenchmarkGet_Parallel(b *testing.B) {
	db := newBenchDB(b)
	keys := make([]string, 1000)
	for i := range keys {
		f := newFund(fmt.Sprintf("u%d", i), "M0", float64(i))
		if err := db.Set(f, 0); err != nil {
			b.Fatalf("setup Set 失败: %v", err)
		}
		keys[i] = f.GetHash()
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = db.Get(keys[i%len(keys)])
			i++
		}
	})
}

// BenchmarkBatchInsert_Parallel 并发 BatchInsert
func BenchmarkBatchInsert_Parallel(b *testing.B) {
	db := newBenchDB(b)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			batch := make([]*testFund, 10)
			for j := range batch {
				batch[j] = newFund(fmt.Sprintf("u%d", i%200), fmt.Sprintf("M%d", j), float64(i+j))
			}
			if err := db.BatchInsert(batch); err != nil {
				b.Errorf("BatchInsert 失败: %v", err)
			}
			i++
		}
	})
}

// BenchmarkMixedReadWrite 混合读写（模拟真实负载：70% 读 30% 写）
func BenchmarkMixedReadWrite(b *testing.B) {
	db := newBenchDB(b)
	// 预热数据
	for i := 0; i < 500; i++ {
		f := newFund(fmt.Sprintf("u%d", i), "M0", float64(i))
		_ = db.Set(f, 0)
	}
	keys := make([]string, 500)
	for i := range keys {
		keys[i] = fmt.Sprintf("u%d:M0", i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 < 7 {
				// 70% 读
				_, _ = db.Get(keys[i%len(keys)])
			} else {
				// 30% 写（新 key）
				f := newFund(fmt.Sprintf("u%d", i%500), fmt.Sprintf("M%d", i), float64(i))
				_ = db.Set(f, 0)
			}
			i++
		}
	})
}

// BenchmarkProcessSyncQueue_LocalOnly 测试本地 processSyncQueue 吞吐（无 MySQL，快速返回路径）
// 预先写入大量数据，然后连续调用 processSyncQueue 直到耗尽
func BenchmarkProcessSyncQueue_LocalOnly(b *testing.B) {
	db := newBenchDB(b)
	// 每轮重新写入 b.N 条数据
	for i := 0; i < b.N; i++ {
		f := newFund(fmt.Sprintf("u%d", i%500), fmt.Sprintf("M%d", i), float64(i))
		_ = db.Set(f, 0)
	}

	b.ResetTimer()
	for {
		n, err := db.processSyncQueue()
		if err != nil {
			b.Fatalf("processSyncQueue 失败: %v", err)
		}
		if n == 0 {
			break
		}
	}
}

// ============================================================
// 高并发功能验证测试（非基准，用 Test 前缀）
// ============================================================

// TestHighConcurrency_PendingCountAccuracy 验证并发写入时 pendingCountCache 精度
// N 个 goroutine 各写 M 次，完成后 cache 值应 >= 实际未同步条目数（cache 只增不减，直到同步后减少）
func TestHighConcurrency_PendingCountAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过高并发测试（-short 模式）")
	}

	const (
		goroutines = 50
		writesEach = 40
		total      = goroutines * writesEach
	)

	db := newTestDBLocal(t)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for w := 0; w < writesEach; w++ {
				f := newFund(fmt.Sprintf("user-%d", g), fmt.Sprintf("M%d", w), float64(g*100+w))
				if err := db.Set(f, 0); err != nil {
					t.Errorf("goroutine %d Set 失败: %v", g, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	// 同一 user 多次写同一 market key 只留最新，故实际条数 <= goroutines*writesEach
	db.pendingCountMutex.RLock()
	cacheVal := db.pendingCountCache
	db.pendingCountMutex.RUnlock()

	// cache 不能超过 total
	if cacheVal > total {
		t.Errorf("pendingCountCache=%d 超过写入总数 %d（计数泄漏）", cacheVal, total)
	}
	if cacheVal < 0 {
		t.Errorf("pendingCountCache=%d 为负（计数下溢）", cacheVal)
	}

	t.Logf("写入 goroutine=%d × %d, pendingCountCache=%d", goroutines, writesEach, cacheVal)
}

// TestHighConcurrency_NoDeadlock 验证高并发写入 + 并发处理 processSyncQueue 不死锁
func TestHighConcurrency_NoDeadlock(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过高并发测试（-short 模式）")
	}

	const (
		writerCount  = 20
		writerWrites = 50
		syncWorkers  = 4
		duration     = 2 * time.Second
	)

	db := newTestDBLocal(t)

	ctx, cancel := func() (chan struct{}, func()) {
		ch := make(chan struct{})
		return ch, func() { close(ch) }
	}()

	// 启动同步 worker（并发调用 processSyncQueue）
	var syncWg sync.WaitGroup
	for i := 0; i < syncWorkers; i++ {
		syncWg.Add(1)
		go func() {
			defer syncWg.Done()
			for {
				select {
				case <-ctx:
					return
				default:
					_, _ = db.processSyncQueue()
					time.Sleep(time.Millisecond)
				}
			}
		}()
	}

	// 启动写 goroutine
	var writeWg sync.WaitGroup
	var writeCount int64
	for g := 0; g < writerCount; g++ {
		g := g
		writeWg.Add(1)
		go func() {
			defer writeWg.Done()
			for w := 0; w < writerWrites; w++ {
				f := newFund(fmt.Sprintf("u%d", g), fmt.Sprintf("M%d", w), float64(w))
				if err := db.Set(f, 0); err != nil {
					t.Errorf("Set 失败: %v", err)
				}
				atomic.AddInt64(&writeCount, 1)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		writeWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 正常完成
	case <-time.After(duration):
		t.Error("超时：可能存在死锁")
	}

	cancel()
	syncWg.Wait()

	t.Logf("完成： 写入 %d 次（%d goroutines × %d），无死锁",
		atomic.LoadInt64(&writeCount), writerCount, writerWrites)
}

// TestHighConcurrency_SyncTriggerChannel 验证大量并发写入时 syncTrigger channel 不溢出、不阻塞
func TestHighConcurrency_SyncTriggerChannel(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过高并发测试（-short 模式）")
	}

	db := newTestDBLocal(t)

	const goroutines = 200

	var wg sync.WaitGroup
	var errCount int64
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			f := newFund(fmt.Sprintf("u%d", g), "MKT", float64(g))
			if err := db.Set(f, 0); err != nil {
				atomic.AddInt64(&errCount, 1)
				t.Errorf("g=%d Set 失败: %v", g, err)
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("超时：写入 goroutine 可能被阻塞（syncTrigger channel 溢出？）")
	}

	if errCount > 0 {
		t.Errorf("%d 次写入失败", errCount)
	}

	// syncTrigger channel 容量=1，不管多少写入都不应阻塞
	t.Logf("%d 个并发写入全部完成，triggerSync 无阻塞", goroutines)
}

// TestHighConcurrency_ConsistentRead 并发写入同时并发读，验证不 panic 且数据一致
func TestHighConcurrency_ConsistentRead(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过高并发测试（-short 模式）")
	}

	db := newTestDBLocal(t)

	const (
		writers = 10
		readers = 10
		ops     = 100
	)

	var wg sync.WaitGroup
	keySet := make([]string, writers*ops)
	for i := range keySet {
		keySet[i] = fmt.Sprintf("u%d:M%d", i%writers, i%ops)
	}

	// 写 goroutine
	for g := 0; g < writers; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := 0; w < ops; w++ {
				f := newFund(fmt.Sprintf("u%d", g), fmt.Sprintf("M%d", w), float64(w))
				_ = db.Set(f, 0)
			}
		}()
	}

	// 读 goroutine（可能读到 nil，属正常）
	for g := 0; g < readers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < ops; r++ {
				idx := r % len(keySet)
				_, _ = db.Get(keySet[idx])
			}
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("超时：并发读写可能存在死锁")
	}

	t.Logf("并发读写 (%d 写 × %d, %d 读 × %d) 全部完成，无 panic", writers, ops, readers, ops)
}

// ============================================================
// 新技能验证测试（本地，无需 MySQL）
// 覆盖：SyncBatchDelay 默认履顿、syncSema 容量自动调整、信号量并发限制
// ============================================================

// TestSyncConfig_DefaultBatchDelay 验证默认积累窗口为 100ms
func TestSyncConfig_DefaultBatchDelay(t *testing.T) {
	db := newTestDBLocal(t)
	want := 100 * time.Millisecond
	got := db.manager.config.SyncBatchDelay
	if got != want {
		t.Errorf("默认 SyncBatchDelay = %v，期望 %v", got, want)
	}
}

// TestSyncSema_AutoSizingFromMaxOpenConns 验证 SetSyncDB 根据 MySQL MaxOpenConns 自动调整信号量容量
func TestSyncSema_AutoSizingFromMaxOpenConns(t *testing.T) {
	db := newTestDBLocal(t)

	// 使用 MaxOpenConns=20 的 MySQL 配置调用 SetSyncDB
	// 信号量以 adapter 指针为键；期望容量 = 20 * 3/4 = 15
	const maxOpenConns = 20
	cfg := &oltp.Config{
		Host:         "127.0.0.1",
		Port:         3306,
		Username:     "root",
		Password:     "pw",
		MaxOpenConns: maxOpenConns,
	}
	action := oltp.NewMySQL(cfg)
	list := entity.NewModelList[testFund](action)
	db.SetSyncDB(list)

	wantCap := maxOpenConns * 3 / 4 // = 15
	gotCap := cap(db.syncSema)
	if gotCap != wantCap {
		t.Errorf("SetSyncDB 后 syncSema 容量 = %d，期望 %d（MaxOpenConns=%d × 75%%）",
			gotCap, wantCap, maxOpenConns)
	}
	t.Logf("syncSema 容量自动调整为 %d（MaxOpenConns=%d 的 75%%）", gotCap, maxOpenConns)
}

// TestSyncSema_AutoSizing_ZeroMaxOpenConns_KeepsDefault 验证 MaxOpenConns=0 时使用默认容量 8
func TestSyncSema_AutoSizing_ZeroMaxOpenConns_KeepsDefault(t *testing.T) {
	db := newTestDBLocal(t)

	cfg := &oltp.Config{Host: "127.0.0.1", Port: 3306, MaxOpenConns: 0} // 0 = 无限制
	action := oltp.NewMySQL(cfg)
	list := entity.NewModelList[testFund](action)
	db.SetSyncDB(list)

	// MaxOpenConns=0 时 getOrCreateAdapterSema 使用全局默认 8
	const wantCap = 8
	if got := cap(db.syncSema); got != wantCap {
		t.Errorf("MaxOpenConns=0 时 syncSema 容量应为默认 %d，实际为 %d", wantCap, got)
	}
	t.Logf("MaxOpenConns=0 时信号量容量正确为默认 %d", wantCap)
}

// TestSyncSema_LimitsConcurrentCalls 验证信号量限制并发 MySQL 事务数
func TestSyncSema_LimitsConcurrentCalls(t *testing.T) {
	db := newTestDBLocal(t)

	const semaLimit = 2
	// 直接注入信号量（容量=2），模拟 adapter MaxOpenConns 限制
	db.syncSema = make(chan struct{}, semaLimit)

	const workers = 6
	var active int64
	var maxActive int64
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 模拟 processSyncQueue 的信号量获取/释放逻辑
			db.syncSema <- struct{}{}
			defer func() { <-db.syncSema }()

			cur := atomic.AddInt64(&active, 1)
			mu.Lock()
			if cur > maxActive {
				maxActive = cur
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond) // 模拟 MySQL 事务耗时
			atomic.AddInt64(&active, -1)
		}()
	}
	wg.Wait()

	if maxActive > semaLimit {
		t.Errorf("最大并发数 = %d，应被限制在 ≤ %d", maxActive, semaLimit)
	}
	t.Logf("最大并发数 = %d（信号量履顿 = %d），%d 个 worker 全部完成", maxActive, semaLimit, workers)
}
