package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/runtime"
	"github.com/stretchr/testify/require"
)

func TestMapStateModeOff(t *testing.T) {
	require.Equal(t, runtime.StateNotCollected, runtime.MapQueryState(runtime.QueryInput{Mode: "off"}))
	require.Equal(t, runtime.StateNotCollected, runtime.MapQueryState(runtime.QueryInput{Mode: ""}))
}

func TestMapStatePrometheusTimeout(t *testing.T) {
	require.Equal(t, runtime.StateUnavailable, runtime.MapQueryState(runtime.QueryInput{
		Mode: "prometheus", Err: context.DeadlineExceeded,
	}))
}

func TestStaleThreshold(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	last := now.Add(-40 * time.Second)
	require.Equal(t, runtime.StateStale, runtime.Freshness("15s", now, &last))
	fresh := now.Add(-5 * time.Second)
	require.Equal(t, runtime.StateOK, runtime.Freshness("15s", now, &fresh))
}

func TestParseWindow(t *testing.T) {
	d, ok := runtime.ParseWindow("5m")
	require.True(t, ok)
	require.Equal(t, 5*time.Minute, d)
	_, ok = runtime.ParseWindow("7d")
	require.False(t, ok)
}

func TestMergeStates(t *testing.T) {
	require.Equal(t, runtime.StateUnavailable, runtime.MergeStates(runtime.StateOK, runtime.StateUnavailable, runtime.StatePartial))
}
