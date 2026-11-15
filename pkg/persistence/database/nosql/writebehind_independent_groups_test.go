// 本文件用真实Badger和受控target验证独立组持续排空，不访问消费方环境。
package nosql

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestIndependentGroupsDrainHealthySuccessor 验证B1真实本地确认后B2推进，无需等待无关A1。
func TestIndependentGroupsDrainHealthySuccessor(t *testing.T) {
	slow, fast, next, release := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseSlow := func() { once.Do(func() { close(release) }) }
	target := &failureTestTarget{}
	target.sync = func(ctx context.Context, items []*SyncQueueItem[testFund]) (*WriteBehindResult, error) {
		var wg sync.WaitGroup
		for _, item := range items {
			wg.Add(1)
			go func(key string) {
				defer wg.Done()
				switch key {
				case "A1":
					close(slow)
					<-release
				case "B1":
					close(fast)
				case "B2":
					close(next)
				}
			}(item.Item.UserID)
		}
		wg.Wait()
		r := &WriteBehindResult{}
		for _, item := range items {
			r.ConfirmedKeys = append(r.ConfirmedKeys, item.Key)
		}
		return r, nil
	}
	target.handle = func(context.Context, WriteBehindFailure[testFund]) (WriteBehindDecision, error) {
		t.Error("成功target不得调用失败处置")
		return WriteBehindDecision{}, nil
	}
	path := t.TempDir()
	cfg := ReliableWriteStoreConfig{BasePath: path, Badger: DefaultProductionConfig(path)}
	cfg.Badger.AutoSync = false
	cfg.Badger.SyncBatchSize = 4
	cfg.Badger.WriteBehindFailure = failureTestPolicy()
	store, _, err := NewReliableWriteStore[testFund](ServiceIdentity{ServiceName: "group-drain", DataCenterID: 1, MachineID: 1}, cfg)
	require.NoError(t, err)
	require.NoError(t, bindIndependentTestStore(store, target))
	defer func() {
		releaseSlow()
		_ = store.Close(context.Background())
		_ = CloseSharedManager(store.config.BasePath)
	}()
	require.NoError(t, store.Save(context.Background(), newFund("A1", "A", 1)))
	require.NoError(t, store.Save(context.Background(), newFund("B1", "B", 1)))
	done := make(chan error, 1)
	go func() { _, e := store.ForceSyncBatch(context.Background(), 4); done <- e }()
	for _, ch := range []chan struct{}{slow, fast} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatal("target未进入")
		}
	}
	require.Eventually(t, func() bool { return store.Metrics().Pending == 1 }, time.Second, time.Millisecond, "B1已返回，但本地确认仍等待A1")
	require.NoError(t, store.Save(context.Background(), newFund("B2", "B", 1)))
	second := make(chan error, 1)
	go func() { _, e := store.ForceSyncBatch(context.Background(), 4); second <- e }()
	select {
	case <-next:
	case <-time.After(time.Second):
		t.Fatal("B2被无关A1阻塞")
	}
	require.NoError(t, <-second)
	require.Equal(t, 1, store.Metrics().Pending)
	releaseSlow()
	require.NoError(t, <-done)
	require.Zero(t, store.Metrics().Pending)
	require.EqualValues(t, 3, store.Metrics().Sync.SyncedItems)
}

func bindIndependentTestStore(s *ReliableWriteStore[testFund], target *failureTestTarget) error {
	return s.UseWriteBehindWithFailureHandlerOptions(target, target, WriteBehindExecutionOptions{IndependentGroups: 2})
}
