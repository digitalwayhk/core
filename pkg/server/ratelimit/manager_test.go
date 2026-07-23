package ratelimit

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestManagerIsolatesRouteAndClient(t *testing.T) {
	manager := NewManager("shop", time.Minute)
	policy := types.ExternalRateLimitPolicy{Rate: 1, Burst: 1}

	require.True(t, manager.Allow("/a", "203.0.113.1", policy))
	require.False(t, manager.Allow("/a", "203.0.113.1", policy))
	require.True(t, manager.Allow("/b", "203.0.113.1", policy))
	require.True(t, manager.Allow("/a", "203.0.113.2", policy))
}

func TestManagerUsesSharedUnknownClientBucket(t *testing.T) {
	manager := NewManager("shop", time.Minute)
	policy := types.ExternalRateLimitPolicy{Rate: 1, Burst: 1}

	require.True(t, manager.Allow("/a", "", policy))
	require.False(t, manager.Allow("/a", "", policy))
}

func TestManagerFailsClosedAfterClose(t *testing.T) {
	manager := NewManager("shop", time.Minute)
	policy := types.ExternalRateLimitPolicy{Rate: 100, Burst: 1}
	require.True(t, manager.Allow("/a", "203.0.113.1", policy))

	manager.Close()
	require.False(t, manager.Allow("/a", "203.0.113.2", policy))
}

func TestManagerRejectsInvalidPolicy(t *testing.T) {
	manager := NewManager("shop", time.Minute)
	require.False(t, manager.Allow("/a", "203.0.113.1", types.ExternalRateLimitPolicy{}))
}
