// 本文件提供按键可查询的待办/进度合同，不把业务key变成指标标签。
package ownership

import (
	"context"
	"time"
)

// KeyStatus 只描述有效本地owner的观测；未知计数为-1，未确认进度时间为nil。
type KeyStatus struct {
	Scope          string
	Key            string
	InstanceID     string
	SessionID      string
	Epoch          int64
	Observation    Observation
	LastConfirmed  *time.Time
	ExecutionSince *time.Time
}

// Status 仅对当前有效本地owner返回状态，非owner应通过CallServiceOwned调用目标实例。
func (r *Runtime) Status(ctx context.Context, key string) (KeyStatus, error) {
	if r == nil || r.ctx.Err() != nil {
		return KeyStatus{}, ErrNotReady
	}
	credential, err := r.store.Resolve(ctx, r.options.Manifest.Scope, key)
	if err != nil {
		return KeyStatus{}, err
	}
	if credential.InstanceID != r.options.InstanceID || credential.SessionID != r.options.SessionID {
		return KeyStatus{}, ErrNotReady
	}
	r.statusMu.RLock()
	state := r.status[key]
	r.statusMu.RUnlock()
	if state == nil || !state.credential.Same(credential) {
		return KeyStatus{}, ErrNotReady
	}
	if state.context == nil || state.context.Err() != nil {
		return KeyStatus{}, ErrNotReady
	}
	observation, err := r.observe(ctx, credential)
	if err != nil {
		return KeyStatus{}, err
	}
	if err := r.store.Validate(ctx, credential, true); err != nil {
		return KeyStatus{}, err
	}
	if state.context.Err() != nil {
		return KeyStatus{}, ErrNotReady
	}
	result := KeyStatus{Scope: credential.Scope, Key: credential.Key, InstanceID: credential.InstanceID, SessionID: credential.SessionID, Epoch: credential.Epoch, Observation: observation}
	if n := state.confirmed.Load(); n > 0 {
		at := time.Unix(0, n).UTC()
		result.LastConfirmed = &at
	}
	if n := state.stageSince.Load(); n > 0 {
		at := time.Unix(0, n).UTC()
		result.ExecutionSince = &at
	}
	return result, nil
}
