package router

import (
	"context"
	"fmt"
	"sync/atomic"
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

type noHMACAuthTestService struct{ name string }

func (s *noHMACAuthTestService) ServiceName() string    { return s.name }
func (*noHMACAuthTestService) Routers() []types.IRouter { return nil }

func (s *authHookTestService) ServiceName() string    { return s.name }
func (*authHookTestService) Routers() []types.IRouter { return nil }
func (s *authHookTestService) OnAuth(_ context.Context, args *types.AuthHookArgs) error {
	s.captured = args
	return nil
}
func (*authHookTestService) OnAuthRequest(context.Context, types.AuthRequestArgs) error { return nil }
func (*authHookTestService) OnCasdoorEvent(context.Context, types.CasdoorEvent) error   { return nil }
func (*authHookTestService) AuthenticateHMAC(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
	return &types.HMACAuthResult{Identity: types.AuthIdentity{UID: "user-1", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "key-1"}}, nil
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
	require.Same(t, service, sc.AuthRequestHookProvider)
	require.Same(t, service, sc.CasdoorEventHookProvider)
	require.Same(t, service, sc.HMACAuthProvider)
}

// TestServiceContextWithoutHMACProviderKeepsRuntimeNil 验证旧服务未实现第四 Hook 时保持零值兼容。
func TestServiceContextWithoutHMACProviderKeepsRuntimeNil(t *testing.T) {
	service := &noHMACAuthTestService{name: fmt.Sprintf("no-hmac-provider-%d", time.Now().UnixNano())}
	cfg := config.NewServiceDefaultConfig(service.name, 31994)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil

	sc := NewServiceContextWithConfig(service, cfg)
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	provider, active := sc.GetHMACAuthRuntime()
	require.True(t, active)
	require.Nil(t, provider)
}

// TestInvokeHMACAuthKeepsSlotUntilIgnoringProviderReturns 验证忽略 ctx 的 Provider 会继续占用名额而不会引发 goroutine 无界增长。
func TestInvokeHMACAuthKeepsSlotUntilIgnoringProviderReturns(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	service := &blockingHMACAuthTestService{
		name: fmt.Sprintf("bounded-hmac-provider-%d", time.Now().UnixNano()),
		authenticate: func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			calls.Add(1)
			<-release
			return nil, nil
		},
	}
	cfg := config.NewServiceDefaultConfig(service.name, 31995)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil
	cfg.Timeout = 5
	cfg.HMACAuth.MaxInFlight = 1
	sc := NewServiceContextWithConfig(service, cfg)
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	_, firstErr := sc.InvokeHMACAuth(context.Background(), types.HMACAuthArgs{})
	_, secondErr := sc.InvokeHMACAuth(context.Background(), types.HMACAuthArgs{})
	close(release)

	require.Equal(t, types.ErrorKindInternal, types.ResolvePublicError(firstErr).Kind)
	require.Equal(t, types.ErrorKindRateLimited, types.ResolvePublicError(secondErr).Kind)
	require.Equal(t, int32(1), calls.Load())
}

// TestSetRunStateCancelsAndWaitsForHMACAuth 验证服务停止会取消并有界等待进行中的 HMAC Hook。
func TestSetRunStateCancelsAndWaitsForHMACAuth(t *testing.T) {
	entered := make(chan struct{})
	canceled := make(chan struct{})
	service := &blockingHMACAuthTestService{
		name: fmt.Sprintf("shutdown-hmac-provider-%d", time.Now().UnixNano()),
		authenticate: func(ctx context.Context, _ types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			close(entered)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		},
	}
	cfg := config.NewServiceDefaultConfig(service.name, 31996)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil
	cfg.Timeout = 5000
	sc := NewServiceContextWithConfig(service, cfg)
	sc.SetRunState(true)
	invokeDone := make(chan struct{})
	go func() {
		defer close(invokeDone)
		_, _ = sc.InvokeHMACAuth(context.Background(), types.HMACAuthArgs{})
	}()
	<-entered

	sc.SetRunState(false)

	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("服务停止应取消 HMAC Provider context")
	}
	<-invokeDone
	require.NoError(t, sc.ShutdownError())
}

// TestInvokeHMACAuthRejectsResultAtDeadline 验证 Provider 与 deadline 同时就绪时仍严格 fail closed。
func TestInvokeHMACAuthRejectsResultAtDeadline(t *testing.T) {
	service := &blockingHMACAuthTestService{
		name: fmt.Sprintf("deadline-hmac-provider-%d", time.Now().UnixNano()),
		authenticate: func(ctx context.Context, _ types.HMACAuthArgs) (*types.HMACAuthResult, error) {
			<-ctx.Done()
			return &types.HMACAuthResult{Identity: types.AuthIdentity{
				UID: "42", AuthType: types.AuthTypeUser, Provider: "apikey", ProviderSubject: "credential-1",
			}}, nil
		},
	}
	cfg := config.NewServiceDefaultConfig(service.name, 31997)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil
	cfg.Timeout = 5
	sc := NewServiceContextWithConfig(service, cfg)
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	result, err := sc.InvokeHMACAuth(context.Background(), types.HMACAuthArgs{})

	require.Nil(t, result)
	require.Equal(t, types.ErrorKindInternal, types.ResolvePublicError(err).Kind)
}

type blockingHMACAuthTestService struct {
	name         string
	authenticate func(context.Context, types.HMACAuthArgs) (*types.HMACAuthResult, error)
}

func (s *blockingHMACAuthTestService) ServiceName() string    { return s.name }
func (*blockingHMACAuthTestService) Routers() []types.IRouter { return nil }
func (s *blockingHMACAuthTestService) AuthenticateHMAC(ctx context.Context, args types.HMACAuthArgs) (*types.HMACAuthResult, error) {
	return s.authenticate(ctx, args)
}
