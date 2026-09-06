package mq

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lifecycleTestProvider struct {
	mu             sync.Mutex
	capabilities   LifecycleCapabilities
	ensureCalls    int
	inspectCalls   int
	reclaimCalls   int
	lastPolicy     LifecyclePolicy
	snapshot       LifecycleSnapshot
	inspectStarted chan struct{}
	blockInspect   bool
	events         []string
}

func (*lifecycleTestProvider) Name() string                  { return "lifecycle-test" }
func (*lifecycleTestProvider) Connect(context.Context) error { return nil }
func (p *lifecycleTestProvider) Close() error {
	p.mu.Lock()
	p.events = append(p.events, "provider-closed")
	p.mu.Unlock()
	return nil
}
func (*lifecycleTestProvider) Publish(context.Context, string, []byte, *PublishOptions) error {
	return nil
}
func (*lifecycleTestProvider) Subscribe(context.Context, string, func(*Message)) (func(), error) {
	return func() {}, nil
}
func (*lifecycleTestProvider) Health(context.Context) error { return nil }
func (p *lifecycleTestProvider) LifecycleCapabilities() LifecycleCapabilities {
	return p.capabilities
}
func (p *lifecycleTestProvider) EnsureLifecycle(_ context.Context, policy LifecyclePolicy) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensureCalls++
	p.lastPolicy = policy
	return nil
}
func (p *lifecycleTestProvider) InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error) {
	p.mu.Lock()
	p.inspectCalls++
	block := p.blockInspect && p.inspectCalls > 1
	started := p.inspectStarted
	snapshot := p.snapshot
	p.mu.Unlock()
	if block {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		p.mu.Lock()
		p.events = append(p.events, "worker-stopped")
		p.mu.Unlock()
		return LifecycleSnapshot{}, ctx.Err()
	}
	if snapshot.Subject == "" {
		snapshot.Subject = policy.Subject
	}
	if snapshot.PolicyFingerprint == "" {
		snapshot.PolicyFingerprint = policy.Fingerprint()
	}
	if snapshot.ObservedAt.IsZero() {
		snapshot.ObservedAt = time.Now()
	}
	if snapshot.State == "" {
		snapshot.State = LifecycleStateOK
	}
	return snapshot, nil
}
func (p *lifecycleTestProvider) ReclaimLifecycle(_ context.Context, policy LifecyclePolicy, _ LifecycleSnapshot) (ReclaimResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reclaimCalls++
	p.lastPolicy = policy
	return ReclaimResult{}, nil
}

func allLifecycleCapabilities() LifecycleCapabilities {
	return LifecycleCapabilities{
		PublishAck: PublishAckBrokerPersisted, RequiredGroups: true, Retry: true, DeadLetter: true,
		SafeReclaim: true, RetainedMessages: true, RetainedBytes: true,
		Pending: true, Lag: true, OldestAge: true,
	}
}

func managerWithLifecycleTestProvider(t *testing.T, provider MQProvider) *MQManager {
	t.Helper()
	manager := NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	t.Cleanup(func() { _ = manager.Close() })
	return manager
}

// TestRequireMessageLifecycleFailsClosedWithoutCapability 验证普通 Provider 不会伪装支持生命周期。
func TestRequireMessageLifecycleFailsClosedWithoutCapability(t *testing.T) {
	manager := managerWithLifecycleTestProvider(t, &lifecycleUnsupportedProvider{})

	err := manager.RequireMessageLifecycle(context.Background(), validLifecyclePolicy("positions"))

	require.ErrorIs(t, err, ErrLifecycleUnsupported)
}

// TestRequireMessageLifecycleRejectsConflictingFrozenPolicy 验证 Subject 策略启动后不能在运行时改写。
func TestRequireMessageLifecycleRejectsConflictingFrozenPolicy(t *testing.T) {
	provider := &lifecycleTestProvider{capabilities: allLifecycleCapabilities()}
	manager := managerWithLifecycleTestProvider(t, provider)
	first := validLifecyclePolicy("positions")
	second := validLifecyclePolicy("users")

	require.NoError(t, manager.RequireMessageLifecycle(context.Background(), first))
	require.ErrorIs(t, manager.RequireMessageLifecycle(context.Background(), second), ErrLifecyclePolicyConflict)
	require.Equal(t, 1, provider.ensureCalls)
}

// TestLifecycleObserveNeverReclaims 验证 observe-only 只更新快照。
func TestLifecycleObserveNeverReclaims(t *testing.T) {
	provider := &lifecycleTestProvider{capabilities: allLifecycleCapabilities()}
	manager := managerWithLifecycleTestProvider(t, provider)
	policy := validLifecyclePolicy("positions")
	policy.Mode = LifecycleModeObserve

	require.NoError(t, manager.RequireMessageLifecycle(context.Background(), policy))
	require.NoError(t, manager.lifecycle.runOnce(context.Background(), policy.Subject))
	require.Zero(t, provider.reclaimCalls)
}

// TestLifecycleEnforcePassesBoundedPolicyToProvider 验证 enforce 回收仍受应用批次和时间预算约束。
func TestLifecycleEnforcePassesBoundedPolicyToProvider(t *testing.T) {
	provider := &lifecycleTestProvider{capabilities: allLifecycleCapabilities()}
	manager := managerWithLifecycleTestProvider(t, provider)
	policy := validLifecyclePolicy("positions")
	policy.Reclaim = ReclaimBudget{Interval: time.Hour, BatchSize: 7, TimeBudget: 20 * time.Millisecond}

	require.NoError(t, manager.RequireMessageLifecycle(context.Background(), policy))
	require.NoError(t, manager.lifecycle.runOnce(context.Background(), policy.Subject))
	require.Equal(t, 1, provider.reclaimCalls)
	require.Equal(t, policy.Reclaim, provider.lastPolicy.Reclaim)
}

// TestPublishRejectsAtFreshHardCapacityAndWhenCapacityUnknown 验证硬限不依赖过期或假零快照。
func TestPublishRejectsAtFreshHardCapacityAndWhenCapacityUnknown(t *testing.T) {
	retained := int64(11)
	provider := &lifecycleTestProvider{
		capabilities: allLifecycleCapabilities(),
		snapshot: LifecycleSnapshot{
			State: LifecycleStateOK, ObservedAt: time.Now(), RetainedMessages: &retained,
		},
	}
	manager := managerWithLifecycleTestProvider(t, provider)
	policy := validLifecyclePolicy("positions")
	policy.Capacity.HardMessages = 10

	require.NoError(t, manager.RequireMessageLifecycle(context.Background(), policy))
	require.ErrorIs(t, manager.Publish(context.Background(), policy.Subject, []byte("x"), nil), ErrLifecycleBackpressure)

	manager.lifecycle.mu.Lock()
	manager.lifecycle.entries[policy.Subject].snapshot.ObservedAt = time.Now().Add(-3 * policy.Reclaim.Interval)
	manager.lifecycle.mu.Unlock()
	require.ErrorIs(t, manager.Publish(context.Background(), policy.Subject, []byte("x"), nil), ErrLifecycleCapacityUnknown)
}

// TestCloseStopsLifecycleWorkersBeforeProviderClose 验证关闭顺序不会让 worker 访问已关闭 Broker client。
func TestCloseStopsLifecycleWorkersBeforeProviderClose(t *testing.T) {
	provider := &lifecycleTestProvider{
		capabilities: allLifecycleCapabilities(), inspectStarted: make(chan struct{}, 1), blockInspect: true,
	}
	manager := NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	policy := validLifecyclePolicy("positions")
	policy.Reclaim.Interval = time.Millisecond
	require.NoError(t, manager.RequireMessageLifecycle(context.Background(), policy))
	select {
	case <-provider.inspectStarted:
	case <-time.After(time.Second):
		t.Fatal("lifecycle worker did not start")
	}

	require.NoError(t, manager.Close())
	provider.mu.Lock()
	events := append([]string(nil), provider.events...)
	provider.mu.Unlock()
	require.Equal(t, []string{"worker-stopped", "provider-closed"}, events)
}

type lifecycleUnsupportedProvider struct{}

func (*lifecycleUnsupportedProvider) Name() string                  { return "unsupported" }
func (*lifecycleUnsupportedProvider) Connect(context.Context) error { return nil }
func (*lifecycleUnsupportedProvider) Close() error                  { return nil }
func (*lifecycleUnsupportedProvider) Publish(context.Context, string, []byte, *PublishOptions) error {
	return nil
}
func (*lifecycleUnsupportedProvider) Subscribe(context.Context, string, func(*Message)) (func(), error) {
	return func() {}, nil
}
func (*lifecycleUnsupportedProvider) Health(context.Context) error { return errors.New("unused") }
