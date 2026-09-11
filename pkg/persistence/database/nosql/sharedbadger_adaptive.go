// 本文件将自适应收集调度接入现有可靠同步事务，不修改远端确认语义。
package nosql

import (
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

func (p *PrefixedBadgerDB[T]) pendingSyncWindow() (int, time.Time) {
	p.pendingCountMutex.RLock()
	defer p.pendingCountMutex.RUnlock()
	return p.pendingCountCache, p.pendingFirstAt
}

// 初始化必须在实例返回调用方之前完成，不能在 worker 中扫描后覆盖并发写入计数。
func (p *PrefixedBadgerDB[T]) initAdaptivePending() error {
	if _, err := p.rebuildSyncQueue(); err != nil {
		return err
	}
	count, err := p.GetPendingSyncCount()
	if err != nil {
		return err
	}
	p.pendingCountCache = count
	// 零时间表示恢复的历史积压：绑定目标后直接排空。
	return nil
}

func (p *PrefixedBadgerDB[T]) syncAdaptive() {
	schedule := adaptiveSyncSchedule{config: p.manager.config}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-p.closeCh:
			return
		default:
		}
		count, first := p.pendingSyncWindow()
		delay := schedule.delay(time.Now(), count, first)
		if delay != 0 {
			if delay < 0 {
				delay = max(time.Second, schedule.config.SyncInterval)
			}
			timer.Reset(delay)
			select {
			case <-p.closeCh:
				return
			case <-p.syncTrigger:
			case <-timer.C:
			}
			continue
		}
		p.syncLock.RLock()
		hasDB := p.syncDB
		p.syncLock.RUnlock()
		if !hasDB {
			select {
			case <-p.closeCh:
				return
			case <-p.syncTrigger:
			}
			continue
		}
		p.syncMutex.Lock()
		p.syncInProgress = true
		p.syncMutex.Unlock()
		confirmed, err := p.processSyncQueue()
		p.syncMutex.Lock()
		p.syncInProgress = false
		p.syncMutex.Unlock()
		schedule.completed(time.Now(), first, confirmed, err)
		if err != nil {
			logx.Errorw("write_behind_adaptive_sync_failed", logx.Field("prefix", p.prefix), logx.Field("confirmed", confirmed))
		}
	}
}
