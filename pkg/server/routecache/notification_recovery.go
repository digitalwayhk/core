// 本文件以权威 generation 对账和本地版本失效补偿瞬时通知丢失，不扫描 Broker 历史。
package routecache

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const notificationFreshness = 5 * time.Second

type notificationStateReader interface{ NotificationState() (uint64, bool) }

func (m *Manager) startNotificationRecovery() {
	if m.config.Mode != "shared" {
		return
	}
	guard, ok := m.events.(notificationStateReader)
	if !ok {
		return
	}
	m.notificationGuard = guard
	epoch, ready := guard.NotificationState()
	m.notificationConnection.Store(epoch)
	// 不复用进程重启前的本地 L2 版本。
	m.notificationEpoch.Store(uint64(time.Now().UnixNano()))
	m.notificationSynced.Store(time.Now().UnixNano())
	if !ready {
		m.state.Store(uint32(StateDegraded))
	}
	m.notificationDone = make(chan struct{})
	go func() {
		defer close(m.notificationDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-m.notificationContext.Done():
				return
			case <-ticker.C:
			}
			ctx, cancel := context.WithTimeout(m.notificationContext, 3*time.Second)
			_, _ = m.reconcileNotification(ctx)
			cancel()
		}
	}()
}

func (m *Manager) notificationAvailable() bool {
	if m.notificationGuard == nil {
		return true
	}
	epoch, ready := m.notificationGuard.NotificationState()
	valid := ready && epoch == m.notificationConnection.Load() && time.Since(time.Unix(0, m.notificationSynced.Load())) < notificationFreshness
	if !valid {
		m.state.CompareAndSwap(uint32(StateEnabled), uint32(StateDegraded))
	}
	return valid
}

func (m *Manager) notificationStillCurrent(epoch uint64) bool {
	return !m.closed.Load() && m.notificationAvailable() && epoch == m.notificationEpoch.Load()
}

func (m *Manager) reconcileNotification(ctx context.Context) (recovered bool, result error) {
	started := time.Now()
	defer func() {
		if observer, ok := m.events.(interface{ RecordNotificationReconcile(bool, time.Duration) }); ok {
			observer.RecordNotificationReconcile(recovered && result == nil, time.Since(started))
		}
	}()
	m.recoveryMu.Lock()
	defer m.recoveryMu.Unlock()
	epoch, ready := m.notificationGuard.NotificationState()
	if !ready || m.closed.Load() {
		m.state.CompareAndSwap(uint32(StateEnabled), uint32(StateDegraded))
		return false, errors.New("notification channel unavailable")
	}
	if !m.redis.Ping(ctx) {
		m.degrade()
		return false, errors.New("route cache Redis ping failed")
	}
	if err := m.refreshSharedGenerations(ctx); err != nil {
		m.degrade()
		return false, err
	}
	current, ready := m.notificationGuard.NotificationState()
	if !ready || current != epoch || m.closed.Load() || ctx.Err() != nil {
		m.state.CompareAndSwap(uint32(StateEnabled), uint32(StateDegraded))
		return false, errors.New("notification recovery superseded")
	}
	// 保持相同物理 key，只改变记录内的本地版本；旧值由原 TTL/容量回收。
	// 不每秒扩展一套 key，也不做全 L2 删除或持有业务热路径的全局锁。
	m.notificationEpoch.Add(1)
	m.notificationConnection.Store(epoch)
	m.notificationSynced.Store(time.Now().UnixNano())
	m.invalidationReady.Store(true)
	m.state.CompareAndSwap(uint32(StateDegraded), uint32(StateEnabled))
	return m.State() == StateEnabled, nil
}

func (m *Manager) notificationValue(value interface{}, epoch uint64) (interface{}, bool) {
	if m.notificationGuard == nil {
		return value, true
	}
	data, ok := value.(json.RawMessage)
	if !ok {
		return nil, false
	}
	var record notificationCacheRecord
	if json.Unmarshal(data, &record) != nil || record.Epoch != epoch {
		return nil, false
	}
	return record.Data, true
}

func (m *Manager) storeNotificationLocal(key string, data json.RawMessage, ttl time.Duration, epoch uint64) error {
	if m.notificationGuard != nil {
		encoded, err := json.Marshal(notificationCacheRecord{Epoch: epoch, Data: data})
		if err != nil {
			return err
		}
		data = encoded
	}
	if m.l2 != nil {
		if err := m.l2.Set(key, data, ttl); err != nil {
			return err
		}
	}
	m.l1.Set(key, data, ttl)
	return nil
}
