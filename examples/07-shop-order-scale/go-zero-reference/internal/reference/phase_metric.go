// 本文件只记录固定阶段的累计尝试与耗时，不把业务ID作为标签。
package reference

import (
	"sync/atomic"
	"time"
)

type phaseMetric struct {
	attempts atomic.Uint64
	nanos    atomic.Uint64
}

func (a *application) observePhase(phase string, start time.Time) {
	if metric := a.phases[phase]; metric != nil {
		metric.attempts.Add(1)
		metric.nanos.Add(uint64(time.Since(start)))
	}
}
