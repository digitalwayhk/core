package router

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestWithExternalRateLimitFreezesPolicy(t *testing.T) {
	info := &types.RouterInfo{Path: "/api/callback", ServiceName: "auth"}
	applyRouterInfoOptions(info, []RouterInfoOption{WithExternalRateLimit(5, 10)})
	info.Freeze("auth")

	policy := info.GetExternalRateLimit()
	require.NotNil(t, policy)
	require.Equal(t, float64(5), policy.Rate)
	require.Equal(t, 10, policy.Burst)

	require.Panics(t, func() {
		applyRouterInfoOptions(info, []RouterInfoOption{WithExternalRateLimit(10, 20)})
	})
	require.Equal(t, policy, info.GetExternalRateLimit())
}

func TestWithExternalRateLimitRejectsInvalidPolicy(t *testing.T) {
	info := &types.RouterInfo{}
	require.Panics(t, func() {
		applyRouterInfoOptions(info, []RouterInfoOption{WithExternalRateLimit(0, 0)})
	})
}
