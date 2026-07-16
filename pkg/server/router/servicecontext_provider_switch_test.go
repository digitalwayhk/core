package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type providerSwitchService struct{ name string }

func (s *providerSwitchService) ServiceName() string                  { return s.name }
func (*providerSwitchService) Routers() []types.IRouter               { return nil }
func (*providerSwitchService) SubscribeRouters() []*types.ObserveArgs { return nil }

type lifecycleBoundarySwitcher struct {
	current        cluster.DiscoveryProvider
	promoteEntered chan struct{}
	releasePromote chan struct{}
	shutdownCalled chan struct{}
	promoteOnce    sync.Once
	shutdownOnce   sync.Once
	completeCount  atomic.Int32
	finalizeCount  atomic.Int32
}

func (s *lifecycleBoundarySwitcher) Current() cluster.DiscoveryProvider { return s.current }
func (*lifecycleBoundarySwitcher) Begin(context.Context, cluster.DiscoveryProvider) error {
	return nil
}
func (s *lifecycleBoundarySwitcher) Complete(context.Context) error {
	s.completeCount.Add(1)
	return nil
}
func (*lifecycleBoundarySwitcher) Rollback(context.Context) error { return nil }
func (s *lifecycleBoundarySwitcher) Promote(ctx context.Context) error {
	s.promoteOnce.Do(func() { close(s.promoteEntered) })
	select {
	case <-s.releasePromote:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *lifecycleBoundarySwitcher) Finalize(context.Context) error {
	s.finalizeCount.Add(1)
	return nil
}
func (s *lifecycleBoundarySwitcher) Shutdown(context.Context) error {
	s.shutdownOnce.Do(func() { close(s.shutdownCalled) })
	return nil
}

type legacyLifecycleSwitcher struct {
	current       cluster.DiscoveryProvider
	completeCount atomic.Int32
	completeStart chan struct{}
	release       chan struct{}
	completeOnce  sync.Once
}

type beginFailureSwitcher struct {
	cluster.ProviderSwitcher
	err error
}

func (s *beginFailureSwitcher) Begin(context.Context, cluster.DiscoveryProvider) error {
	return s.err
}

type rejectedTargetProvider struct {
	cluster.DiscoveryProvider
	closeCount atomic.Int32
	closeErr   error
}

func (p *rejectedTargetProvider) Name() string { return "rejected" }
func (p *rejectedTargetProvider) Close() error {
	p.closeCount.Add(1)
	return p.closeErr
}

func (s *legacyLifecycleSwitcher) Current() cluster.DiscoveryProvider { return s.current }
func (*legacyLifecycleSwitcher) Begin(context.Context, cluster.DiscoveryProvider) error {
	return nil
}
func (s *legacyLifecycleSwitcher) Complete(context.Context) error {
	s.completeCount.Add(1)
	if s.completeStart != nil {
		s.completeOnce.Do(func() { close(s.completeStart) })
		<-s.release
	}
	return nil
}
func (*legacyLifecycleSwitcher) Rollback(context.Context) error { return nil }

type shutdownPendingProvider struct {
	cluster.DiscoveryProvider
	registerStarted   chan struct{}
	registerExited    chan struct{}
	registerOnce      sync.Once
	registerExitOnce  sync.Once
	closeCount        atomic.Int32
	closeContextCount atomic.Int32
}

func (p *shutdownPendingProvider) Register(ctx context.Context, _ *cluster.NodeInfo) error {
	p.registerOnce.Do(func() { close(p.registerStarted) })
	<-ctx.Done()
	p.registerExitOnce.Do(func() { close(p.registerExited) })
	return ctx.Err()
}
func (p *shutdownPendingProvider) Close() error {
	p.closeCount.Add(1)
	return errors.New("不应调用普通 Close")
}
func (p *shutdownPendingProvider) CloseContext(ctx context.Context) error {
	p.closeContextCount.Add(1)
	if err := ctx.Err(); err != nil {
		return err
	}
	return p.DiscoveryProvider.Close()
}

func newProviderSwitchContext(t *testing.T, suffix string) *ServiceContext {
	t.Helper()
	name := fmt.Sprintf("provider-switch-%s-%d", suffix, time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(name, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	return NewServiceContextWithConfig(&providerSwitchService{name: name}, cfg)
}

func TestCompleteProviderSwitchSerializesTransactionalFlowWithShutdown(t *testing.T) {
	sc := newProviderSwitchContext(t, "transaction-shutdown")
	switcher := &lifecycleBoundarySwitcher{
		current:        sc.ClusterProvider,
		promoteEntered: make(chan struct{}),
		releasePromote: make(chan struct{}),
		shutdownCalled: make(chan struct{}),
	}
	sc.ClusterSwitcher = switcher

	completeDone := make(chan error, 1)
	go func() { completeDone <- sc.CompleteProviderSwitch(context.Background()) }()
	<-switcher.promoteEntered
	shutdownStarted := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		close(shutdownStarted)
		sc.SetRunState(false)
		close(shutdownDone)
	}()
	<-shutdownStarted

	select {
	case <-switcher.shutdownCalled:
		t.Fatal("shutdown 在 provider complete 的 lifecycleOp 结束前进入")
	case <-time.After(30 * time.Millisecond):
	}
	close(switcher.releasePromote)
	require.NoError(t, <-completeDone)
	<-shutdownDone
	assert.Equal(t, int32(1), switcher.finalizeCount.Load())
	assert.Zero(t, switcher.completeCount.Load())
}

func TestCompleteProviderSwitchSerializesLegacyFlowWithShutdown(t *testing.T) {
	sc := newProviderSwitchContext(t, "legacy-shutdown")
	newProvider := cluster.NewLocalProvider(time.Hour, time.Hour, time.Hour)
	switcher := &legacyLifecycleSwitcher{
		current:       newProvider,
		completeStart: make(chan struct{}),
		release:       make(chan struct{}),
	}
	sc.ClusterSwitcher = switcher

	completeDone := make(chan error, 1)
	go func() { completeDone <- sc.CompleteProviderSwitch(context.Background()) }()
	<-switcher.completeStart
	shutdownStarted := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		close(shutdownStarted)
		sc.SetRunState(false)
		close(shutdownDone)
	}()
	<-shutdownStarted

	select {
	case <-shutdownDone:
		t.Fatal("shutdown 在 legacy Complete 的 lifecycleOp 结束前返回")
	case <-time.After(30 * time.Millisecond):
	}
	close(switcher.release)
	require.NoError(t, <-completeDone)
	<-shutdownDone
	assert.Equal(t, int32(1), switcher.completeCount.Load())
}

func TestBeginProviderSwitchClosesTargetWhenBeginFails(t *testing.T) {
	sc := newProviderSwitchContext(t, "begin-failure")
	beginErr := errors.New("begin failed")
	closeErr := errors.New("close failed")
	target := &rejectedTargetProvider{closeErr: closeErr}
	sc.ClusterSwitcher = &beginFailureSwitcher{err: beginErr}

	err := sc.BeginProviderSwitch(context.Background(), target)

	require.ErrorIs(t, err, beginErr)
	assert.ErrorIs(t, err, closeErr)
	assert.Equal(t, int32(1), target.closeCount.Load())
	sc.SetRunState(false)
}

func TestCompleteProviderSwitchRejectsLegacySwitcherAfterShutdown(t *testing.T) {
	sc := newProviderSwitchContext(t, "legacy-terminated")
	switcher := &legacyLifecycleSwitcher{current: sc.ClusterProvider}
	sc.ClusterSwitcher = switcher
	sc.SetRunState(false)

	err := sc.CompleteProviderSwitch(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "terminated")
	assert.Zero(t, switcher.completeCount.Load())
}

func TestServiceContextShutdownCancelsMigrationAndClosesPending(t *testing.T) {
	sc := newProviderSwitchContext(t, "active-migration")
	current := cluster.NewLocalProvider(time.Hour, time.Hour, time.Hour)
	pendingBase := cluster.NewLocalProvider(time.Hour, time.Hour, time.Hour)
	pending := &shutdownPendingProvider{
		DiscoveryProvider: pendingBase,
		registerStarted:   make(chan struct{}),
		registerExited:    make(chan struct{}),
	}
	sc.ClusterProvider = current
	sc.ClusterSwitcher = cluster.NewClusterSwitcher(current, sc.Service.Name)
	sc.ServiceResolver.SetProvider(current)
	sc.SetLifecycleTimeout(250 * time.Millisecond)
	require.NoError(t, sc.ClusterSwitcher.Begin(context.Background(), pending))
	require.NoError(t, current.Register(context.Background(), &cluster.NodeInfo{
		ID: sc.Service.Name + "-node", ServiceName: sc.Service.Name, Weight: 1,
	}))
	<-pending.registerStarted

	sc.SetRunState(false)

	select {
	case <-pending.registerExited:
	default:
		t.Fatal("shutdown 在对账 Register 退出前返回")
	}
	assert.Zero(t, pending.closeCount.Load())
	assert.Equal(t, int32(1), pending.closeContextCount.Load())
	assert.NoError(t, sc.ShutdownError())
	sc.SetRunState(false)
	assert.Equal(t, int32(1), pending.closeContextCount.Load(), "重复 shutdown 不得重复关闭 pending")
	_ = current.Close()
}
