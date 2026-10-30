// 本文件使用真实 ReliableWriteStore 和临时 Badger 验证成功确认后的本地回收，不连接外部服务。
package nosql

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

// cleanupTestRecord 的删除条件随版本持久化，计数和受控屏障只放在测试注册表中。
type cleanupTestRecord struct {
	*entity.Model
	Key         string
	Group       string
	ProbeID     string
	Revision    int
	DeleteAfter bool
}

// NewModel 初始化真实模型基座。
func (r *cleanupTestRecord) NewModel() {
	if r.Model == nil {
		r.Model = entity.NewModel()
	}
}

// GetLocalKey 保持更新前后的本地记录身份一致。
func (r *cleanupTestRecord) GetLocalKey() string { return r.Key }

// IsSyncAfterDelete 记录调用并返回原确认版本自己的回收条件。
func (r *cleanupTestRecord) IsSyncAfterDelete() bool {
	if value, ok := cleanupTestProbes.Load(r.ProbeID); ok {
		probe := value.(*cleanupTestProbe)
		probe.calls.Add(1)
		if probe.hook != nil {
			probe.hook(r)
		}
	}
	return r.DeleteAfter
}

type cleanupTestProbe struct {
	calls atomic.Int32
	hook  func(*cleanupTestRecord)
}

var cleanupTestProbes sync.Map

type cleanupTestTarget struct {
	sync   func(context.Context, []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error)
	handle func(context.Context, WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error)
}

// SyncBatch 由测试显式控制确认结果；不执行 SQL、Redis 或网络操作。
func (a *cleanupTestTarget) SyncBatch(ctx context.Context, items []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
	if a.sync != nil {
		return a.sync(ctx, items)
	}
	result := &WriteBehindResult{}
	for _, item := range items {
		result.ConfirmedKeys = append(result.ConfirmedKeys, item.Key)
	}
	return result, nil
}

// OrderingGroup 只使用测试记录自己的稳定组，不从业务身份推断顺序。
func (*cleanupTestTarget) OrderingGroup(item *SyncQueueItem[cleanupTestRecord]) (string, error) {
	return item.Item.Group, nil
}

// HandleFailure 默认暂停原件，测试需要确认时必须显式给出 Committed 决策。
func (a *cleanupTestTarget) HandleFailure(ctx context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
	if a.handle != nil {
		return a.handle(ctx, f)
	}
	return cleanupTestDecision(f.Record, WriteBehindPause), nil
}

func cleanupTestDecision(r WriteBehindFailureRecord[cleanupTestRecord], action WriteBehindFailureAction) WriteBehindDecision {
	return WriteBehindDecision{ID: r.ID, Version: r.Snapshot.UpdatedAt, Fingerprint: r.Fingerprint, Scope: WriteBehindScopeKey, Action: action}
}

func newCleanupTestRecord(t *testing.T, key string, revision int, deleteAfter bool, probe *cleanupTestProbe) *cleanupTestRecord {
	t.Helper()
	id := fmt.Sprintf("%s/%s/%d", t.Name(), key, revision)
	cleanupTestProbes.Store(id, probe)
	t.Cleanup(func() { cleanupTestProbes.Delete(id) })
	return &cleanupTestRecord{Model: entity.NewModel(), Key: key, Group: key, ProbeID: id, Revision: revision, DeleteAfter: deleteAfter}
}

func openCleanupTestStore(t *testing.T, base string, auto, bounded bool, target *cleanupTestTarget) *ReliableWriteStore[cleanupTestRecord] {
	t.Helper()
	cfg := DefaultProductionConfig(base)
	cfg.AutoSync, cfg.EnableLogger = auto, false
	cfg.SyncInterval = 10 * time.Millisecond
	cfg.SyncMinInterval = time.Millisecond
	cfg.SyncMaxInterval = 100 * time.Millisecond
	cfg.SyncBatchSize = 16
	if bounded {
		cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "cleanup-test", MaxAttempts: 3, MinRetryInterval: time.Millisecond, MaxRetryInterval: 10 * time.Millisecond, HandlerMaxAttempts: 2, HandlerTimeout: time.Second, HandlerWorkers: 1, ScanLimit: 16}
	}
	store, _, err := NewReliableWriteStore[cleanupTestRecord](ServiceIdentity{ServiceName: "cleanup", DataCenterID: 1, MachineID: 1}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg, CloseTimeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close(context.Background())
		_ = CloseSharedManager(store.config.BasePath)
	})
	if bounded {
		err = store.UseWriteBehindWithFailureHandler(target, target)
	} else {
		err = store.UseWriteBehind(target)
	}
	require.NoError(t, err)
	return store
}

// TestWriteBehindLocalCleanupContract 移植消费方对照：普通/有界、手动/自动、target/应用确认均执行原版本 hook。
func TestWriteBehindLocalCleanupContract(t *testing.T) {
	for _, mode := range []string{"legacy", "bounded_target", "bounded_confirm"} {
		for _, auto := range []bool{false, true} {
			for _, deleteAfter := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/auto=%t/delete=%t", mode, auto, deleteAfter), func(t *testing.T) {
					probe := &cleanupTestProbe{}
					record := newCleanupTestRecord(t, "original", 1, deleteAfter, probe)
					target := &cleanupTestTarget{}
					originalErr := errors.New("测试远端结果需要应用确认")
					if mode == "bounded_confirm" {
						target.sync = func(context.Context, []*SyncQueueItem[cleanupTestRecord]) (*WriteBehindResult, error) {
							return nil, originalErr
						}
						target.handle = func(_ context.Context, f WriteBehindFailure[cleanupTestRecord]) (WriteBehindDecision, error) {
							d := cleanupTestDecision(f.Record, WriteBehindConfirm)
							d.Outcome = WriteBehindCommitted
							return d, nil
						}
					}
					store := openCleanupTestStore(t, t.TempDir(), auto, mode != "legacy", target)
					ctx := context.Background()
					require.NoError(t, store.Save(ctx, record))
					if !auto {
						result, err := store.ForceSyncBatch(ctx, 16)
						if mode == "bounded_confirm" {
							require.ErrorIs(t, err, originalErr)
						} else {
							require.NoError(t, err)
							require.Equal(t, 1, result.Confirmed)
						}
					}
					require.Eventually(t, func() bool { return store.Metrics().Pending == 0 }, time.Second, time.Millisecond)
					defer func() { t.Logf("pending=%d hook_calls=%d", store.Metrics().Pending, probe.calls.Load()) }()
					require.Eventually(t, func() bool { return probe.calls.Load() == 1 }, time.Second, time.Millisecond, "实际成功确认必须调度已有 IsSyncAfterDelete")
					if deleteAfter {
						require.Eventually(t, func() bool {
							_, err := store.GetLocal(ctx, record.Key)
							return errors.Is(err, badger.ErrKeyNotFound)
						}, time.Second, time.Millisecond)
					} else {
						local, err := store.GetLocal(ctx, record.Key)
						require.NoError(t, err)
						require.Equal(t, 1, local.Revision)
						wrapper, err := store.db.getWrapper(store.db.generateKey(record))
						require.NoError(t, err)
						require.True(t, wrapper.IsSynced)
					}
				})
			}
		}
	}
}
