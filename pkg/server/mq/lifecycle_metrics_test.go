package mq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLifecycleUnknownMetricsRemainNotCollectedInsteadOfZero(t *testing.T) {
	snapshot := lifecycleRuntimeSnapshot(LifecycleSnapshot{State: LifecycleStateNotCollected})

	require.Equal(t, "not_collected", snapshot.State)
	require.NotContains(t, snapshot.Gauges, "retained_messages")
	require.NotContains(t, snapshot.Gauges, "retained_bytes")
	require.NotContains(t, snapshot.Gauges, "pending_messages")
}

func TestLifecycleMetricsExposeOnlyAggregateLowCardinalityNames(t *testing.T) {
	retained := int64(7)
	bytes := int64(128)
	pending := int64(2)
	oldest := 3 * time.Second
	snapshot := lifecycleRuntimeSnapshot(LifecycleSnapshot{
		Subject: "fills.message-123", State: LifecycleStateOK,
		RetainedMessages: &retained, RetainedBytes: &bytes,
		PendingMessages: &pending, OldestAge: &oldest,
		Groups: []ConsumerGroupSnapshot{{Name: "user-secret-payload"}},
	})

	require.Equal(t, map[string]float64{
		"retained_messages": 7,
		"retained_bytes":    128,
		"pending_messages":  2,
		"oldest_age_sec":    3,
	}, snapshot.Gauges)
	for name := range snapshot.Gauges {
		require.NotContains(t, name, "message-123")
		require.NotContains(t, name, "secret-payload")
	}
}
