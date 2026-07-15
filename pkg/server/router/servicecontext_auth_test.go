package router

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type authHookTestService struct {
	name     string
	captured *types.AuthHookArgs
}

func (s *authHookTestService) ServiceName() string                  { return s.name }
func (*authHookTestService) Routers() []types.IRouter               { return nil }
func (*authHookTestService) SubscribeRouters() []*types.ObserveArgs { return nil }
func (s *authHookTestService) OnAuth(_ context.Context, args *types.AuthHookArgs) error {
	s.captured = args
	return nil
}

func TestServiceContextCapturesAuthHookProvider(t *testing.T) {
	name := fmt.Sprintf("auth-hook-provider-%d", time.Now().UnixNano())
	service := &authHookTestService{name: name}
	cfg := config.NewServiceDefaultConfig(name, 31993)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil

	sc := NewServiceContextWithConfig(service, cfg)
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	require.Same(t, service, sc.AuthHookProvider)
}
