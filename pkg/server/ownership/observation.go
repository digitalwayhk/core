// 本文件定义按归属键的工作观测；-1表示未知，不将全主题lag分摊到key。
package ownership

import "time"

// Observation 同时覆盖已交付、未读、未调度和应用到期任务，未知数量为-1。
type Observation struct {
	At        time.Time
	Delivered int64
	Unread    int64
	Queued    int64
	Due       int64
	// WorkVisible表示有限扫描确认至少有工作；不把未知总数量伪装为准确计数。
	WorkVisible bool
}

// Known 表示本次观测覆盖所有来源。
func (o Observation) Known() bool {
	return !o.At.IsZero() && o.Delivered >= 0 && o.Unread >= 0 && o.Queued >= 0 && o.Due >= 0
}

// HasWork 只用已确认的正数判断待办。
func (o Observation) HasWork() bool {
	return o.WorkVisible || o.Delivered > 0 || o.Unread > 0 || o.Queued > 0 || o.Due > 0
}

// Check 对真实空闲保留租约，待办无进展或持续未知时撤销有效性。
func (o Observation) Check(now, lastProgress, lastKnown time.Time, t Timing) error {
	if o.HasWork() && now.Sub(lastProgress) >= t.Stall {
		return ErrStalled
	}
	if !o.Known() && !o.HasWork() && now.Sub(lastKnown) >= t.Observation {
		return ErrUnknown
	}
	return nil
}
