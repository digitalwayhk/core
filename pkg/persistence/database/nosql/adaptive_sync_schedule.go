// 本文件定义自适应 write-behind 的收集、排空和失败退避时间状态。
package nosql

import "time"

type adaptiveSyncSchedule struct {
	config     BadgerDBConfig
	drainEpoch time.Time
	next       time.Time
	retryDelay time.Duration
}

func (s *adaptiveSyncSchedule) delay(now time.Time, count int, first time.Time) time.Duration {
	if count <= 0 {
		return -1
	}
	// 新信号不能绕过失败退避，阈值也不能绕过显式积压间隔。
	if s.retryDelay > 0 || (!s.next.IsZero() && s.drainEpoch.Equal(first)) {
		return max(0, s.next.Sub(now))
	}
	if first.IsZero() || count >= s.config.SyncFlushThreshold {
		return 0
	}
	return max(0, first.Add(s.config.SyncMaxCollectDelay).Sub(now))
}

func (s *adaptiveSyncSchedule) completed(now, epoch time.Time, confirmed int, err error) {
	if err != nil || confirmed == 0 {
		ceiling := max(s.config.SyncMinInterval, s.config.SyncMaxInterval)
		if s.retryDelay == 0 {
			s.retryDelay = s.config.SyncMinInterval
		} else if s.retryDelay >= ceiling/2 {
			s.retryDelay = ceiling
		} else {
			s.retryDelay *= 2
		}
		s.next = now.Add(s.retryDelay)
		return
	}
	s.retryDelay = 0
	s.drainEpoch = epoch
	s.next = now.Add(s.config.SyncBacklogDrainDelay)
}
