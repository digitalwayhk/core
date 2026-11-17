// 本文件验证显式阻挡分类、零值和公开JSON兼容，不把未完成计数改成空闲。
package ownership

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBlockedDueObservationContract(t *testing.T) {
	now := time.Now()
	base := Observation{At: now, Due: 1, BlockedDue: 1}
	timing := DefaultTiming()
	tests := []struct {
		name   string
		change func(*Observation)
		want   error
	}{
		{"only_persistently_blocked", func(*Observation) {}, nil},
		{"legacy_zero", func(o *Observation) { o.BlockedDue = 0 }, ErrStalled},
		{"one_runnable_due", func(o *Observation) { o.Due = 2 }, ErrStalled},
		{"actual_delivered", func(o *Observation) { o.Delivered = 1 }, ErrStalled},
		{"unread", func(o *Observation) { o.Unread = 1 }, ErrStalled},
		{"queued", func(o *Observation) { o.Queued = 1 }, ErrStalled},
		{"finite_visible", func(o *Observation) { o.WorkVisible = true }, ErrStalled},
		{"capacity_unknown", func(o *Observation) { o.CapacityBlocked = true }, ErrStalled},
		{"other_source_unknown", func(o *Observation) { o.Unread = -1 }, ErrStalled},
		{"stale_blocked_proof", func(o *Observation) { o.At = now.Add(-timing.Observation - time.Second) }, ErrStalled},
		{"future_blocked_proof", func(o *Observation) { o.At = now.Add(timing.Renew + time.Second) }, ErrStalled},
		{"negative_blocked", func(o *Observation) { o.BlockedDue = -1 }, ErrUnknown},
		{"blocked_exceeds_due", func(o *Observation) { o.BlockedDue = 2 }, ErrUnknown},
		{"blocked_without_due", func(o *Observation) { o.Due = 0 }, ErrUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := base
			tt.change(&o)
			require.True(t, o.HasWork() || tt.name == "blocked_without_due")
			err := o.Check(now, now.Add(-time.Hour), now, timing)
			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.True(t, errors.Is(err, tt.want), "%v", err)
			}
		})
	}
	require.True(t, base.Known())
	require.True(t, base.HasWork())
	require.True(t, base.onlyBlockedDue())
}

func TestBlockedDueZeroPreservesObservationJSON(t *testing.T) {
	at := time.Unix(0, 0).UTC()
	old := struct {
		At                             time.Time
		Delivered, Unread, Queued, Due int64
		WorkVisible, CapacityBlocked   bool
	}{At: at, Due: 1}
	expected, err := json.Marshal(old)
	require.NoError(t, err)
	actual, err := json.Marshal(Observation{At: at, Due: 1})
	require.NoError(t, err)
	require.Equal(t, string(expected), string(actual), "旧零值JSON不得悄悄增加字段")
	actual, err = json.Marshal(Observation{At: at, Due: 1, BlockedDue: 1})
	require.NoError(t, err)
	require.Contains(t, string(actual), `"Due":1`)
	require.Contains(t, string(actual), `"BlockedDue":1`)
}
