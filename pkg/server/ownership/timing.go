// 本文件区分租约存活、实际执行和观测失联，不以空闲误判卡死。
package ownership

import "time"

// Timing 是明确的有界时限；不是业务延迟SLO。
type Timing struct {
	Lease       time.Duration
	Renew       time.Duration
	Poll        time.Duration
	Execution   time.Duration
	Stall       time.Duration
	Observation time.Duration
}

// DefaultTiming 返回15s租约/3s续租/250ms轮询/30s执行/45s待办停滞/15s观测失联。
func DefaultTiming() Timing {
	return Timing{15 * time.Second, 3 * time.Second, 250 * time.Millisecond, 30 * time.Second, 45 * time.Second, 15 * time.Second}
}

// Normalize 只将全零时限替换为默认，不悄悄修正错误显式值。
func (t Timing) Normalize() Timing {
	if t == (Timing{}) {
		return DefaultTiming()
	}
	return t
}

// Validate 验证续租早于租约，观测和待办时限有界。
func (t Timing) Validate() error {
	if t.Lease < time.Millisecond || t.Renew < time.Millisecond || t.Renew >= t.Lease/2 || t.Poll < time.Millisecond || t.Execution < time.Millisecond || t.Stall < t.Execution || t.Observation < time.Millisecond {
		return ErrUnknown
	}
	return nil
}
