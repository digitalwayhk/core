package router

import (
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type rateLimitContextService struct{ name string }

func (s *rateLimitContextService) ServiceName() string                  { return s.name }
func (*rateLimitContextService) Routers() []types.IRouter               { return nil }
func (*rateLimitContextService) SubscribeRouters() []*types.ObserveArgs { return nil }

func TestServiceContextOwnsAndClosesPublicRateLimiter(t *testing.T) {
	name := fmt.Sprintf("rate-limit-owner-%d", time.Now().UnixNano())
	service := &rateLimitContextService{name: name}
	cfg := config.NewServiceDefaultConfig(name, 31995)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil

	sc := NewServiceContextWithConfig(service, cfg)
	require.NotNil(t, sc.PublicRateLimiter)
	manager := sc.PublicRateLimiter
	policy := types.ExternalRateLimitPolicy{Rate: 1, Burst: 1}
	require.True(t, manager.Allow("/api/health", "198.51.100.10", policy))

	sc.SetRunState(true)
	sc.SetRunState(false)
	require.Nil(t, sc.PublicRateLimiter)
	require.False(t, manager.Allow("/api/health", "198.51.100.11", policy))
}
