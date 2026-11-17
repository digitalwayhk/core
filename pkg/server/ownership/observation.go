// 本文件定义按归属键的工作观测；-1表示未知，不将全主题lag分摊到key。
package ownership

import "time"

// Observation同时覆盖已交付、未读、未调度和到期任务；主计数未知为-1，BlockedDue仅接受非负已核实子集。
type Observation struct {
	At        time.Time
	Delivered int64
	Unread    int64
	Queued    int64
	Due       int64
	// WorkVisible表示有限扫描确认至少有工作；不把未知总数量伪装为准确计数。
	WorkVisible bool
	// CapacityBlocked表示元数据有效但有限窗口无法看全；不等于Broker/目录损坏。
	CapacityBlocked bool
	// BlockedDue仅为Due中应用已由持久事实核实的业务阻挡子集；0保持旧停滞语义，不覆盖未知或在途工作。
	BlockedDue int64 `json:",omitempty"`
}

// Known 表示本次观测覆盖所有来源。
func (o Observation) Known() bool {
	return !o.At.IsZero() && o.Delivered >= 0 && o.Unread >= 0 && o.Queued >= 0 && o.Due >= 0 && o.validBlockedDue()
}

// HasWork 只用已确认的正数判断待办。
func (o Observation) HasWork() bool {
	return o.WorkVisible || o.Delivered > 0 || o.Unread > 0 || o.Queued > 0 || o.Due > 0
}

// Check 对真实空闲保留租约，待办无进展或持续未知时撤销有效性。
func (o Observation) Check(now, lastProgress, lastKnown time.Time, t Timing) error {
	return o.check(now, lastProgress, lastKnown, t, true)
}

// check不允许回调错误借业务阻挡分类无限保持租约。
func (o Observation) check(now, lastProgress, lastKnown time.Time, t Timing, allowBlocked bool) error {
	if !o.validBlockedDue() {
		return ErrUnknown
	}
	blockedOnly := allowBlocked && o.onlyBlockedDue() && !o.At.After(now.Add(t.Renew)) && now.Sub(o.At) <= t.Observation
	if o.HasWork() && !blockedOnly && now.Sub(lastProgress) >= t.Stall {
		return ErrStalled
	}
	if !o.Known() && !o.HasWork() && !o.CapacityBlocked && now.Sub(lastKnown) >= t.Observation {
		return ErrUnknown
	}
	return nil
}

// validBlockedDue不把未知数量或越界分类当作阻挡证明。
func (o Observation) validBlockedDue() bool {
	return o.BlockedDue >= 0 && (o.BlockedDue == 0 || o.Due >= o.BlockedDue)
}

// onlyBlockedDue要求全来源已知；其他已知/有限未知工作不能被Due的子集屏蔽。
func (o Observation) onlyBlockedDue() bool {
	return o.Known() && o.Due > 0 && o.BlockedDue == o.Due && o.Delivered == 0 && o.Unread == 0 && o.Queued == 0 && !o.WorkVisible && !o.CapacityBlocked
}
