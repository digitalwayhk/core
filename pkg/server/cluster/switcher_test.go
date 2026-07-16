package cluster_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type switcherTestProvider struct {
	cluster.DiscoveryProvider
	listErr          error
	watchErr         error
	registerFailures atomic.Int32
	blockRegister    bool
	registerStarted  chan struct{}
	registerExited   chan struct{}
	registerOnce     sync.Once
}

type closeTrackingProvider struct {
	cluster.DiscoveryProvider
	closeCount atomic.Int32
}

func (p *closeTrackingProvider) Close() error {
	p.closeCount.Add(1)
	return p.DiscoveryProvider.Close()
}

type retryCloseProvider struct {
	cluster.DiscoveryProvider
	closeCount atomic.Int32
	failFirst  atomic.Bool
	block      <-chan struct{}
}

type contextCloseProvider struct {
	cluster.DiscoveryProvider
	closeCount        atomic.Int32
	closeContextCount atomic.Int32
}

func (p *contextCloseProvider) Close() error {
	p.closeCount.Add(1)
	return errors.New("不应调用普通 Close")
}

func (p *contextCloseProvider) CloseContext(context.Context) error {
	p.closeContextCount.Add(1)
	return p.DiscoveryProvider.Close()
}

func (p *retryCloseProvider) Close() error {
	p.closeCount.Add(1)
	if p.block != nil {
		<-p.block
	}
	if p.failFirst.CompareAndSwap(true, false) {
		return errors.New("模拟首次关闭失败")
	}
	return p.DiscoveryProvider.Close()
}

func (p *switcherTestProvider) List(
	ctx context.Context,
	serviceName string,
	statuses ...cluster.NodeStatus,
) ([]*cluster.NodeInfo, error) {
	if p.listErr != nil {
		return nil, p.listErr
	}
	return p.DiscoveryProvider.List(ctx, serviceName, statuses...)
}

func (p *switcherTestProvider) Register(ctx context.Context, node *cluster.NodeInfo) error {
	if p.blockRegister {
		p.registerOnce.Do(func() { close(p.registerStarted) })
		<-ctx.Done()
		close(p.registerExited)
		return ctx.Err()
	}
	if p.registerFailures.Add(-1) >= 0 {
		return errors.New("模拟 pending 注册失败")
	}
	return p.DiscoveryProvider.Register(ctx, node)
}

func (p *switcherTestProvider) Watch(
	ctx context.Context,
	serviceName string,
	onChange func([]*cluster.NodeInfo),
) (func(), error) {
	if p.watchErr != nil {
		return nil, p.watchErr
	}
	return p.DiscoveryProvider.Watch(ctx, serviceName, onChange)
}

func TestClusterSwitcher_CompletePromotesPendingProvider(t *testing.T) {
	ctx := context.Background()
	provA := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provB := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provA.Start()
	provB.Start()
	defer provA.Close()
	defer provB.Close()

	switcher := cluster.NewClusterSwitcher(provA, "svc")
	require.NoError(t, switcher.Begin(ctx, provB))
	require.NoError(t, switcher.Complete(ctx))

	assert.Same(t, provB, switcher.Current())
	assert.Equal(t, provB.Name(), switcher.Current().Name())
}

func TestClusterSwitcher_CompleteClosesOldProviderForLegacyCallers(t *testing.T) {
	ctx := context.Background()
	oldBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	old := &closeTrackingProvider{DiscoveryProvider: oldBase}
	pending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	switcher := cluster.NewClusterSwitcher(old, "svc")
	require.NoError(t, switcher.Begin(ctx, pending))
	require.NoError(t, switcher.Complete(ctx))
	assert.Same(t, pending, switcher.Current())
	assert.Equal(t, int32(1), old.closeCount.Load(), "仅持有 ProviderSwitcher 的调用方不得泄漏旧 provider")
	_ = pending.Close()
}

func TestClusterSwitcher_TransactionPromoteDefersOldCloseUntilFinalize(t *testing.T) {
	ctx := context.Background()
	oldBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	old := &closeTrackingProvider{DiscoveryProvider: oldBase}
	pending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	switcher := cluster.NewClusterSwitcher(old, "svc")
	transaction, ok := switcher.(cluster.ProviderSwitchTransaction)
	require.True(t, ok)
	require.NoError(t, switcher.Begin(ctx, pending))
	require.NoError(t, transaction.Promote(ctx))
	require.NoError(t, transaction.Promote(ctx), "已提升且 retired 存在时 Promote 必须幂等")
	assert.Same(t, pending, switcher.Current())
	assert.Zero(t, old.closeCount.Load(), "Promote 后必须保留旧 provider 供运行时同步")

	require.NoError(t, transaction.Finalize(ctx))
	assert.Equal(t, int32(1), old.closeCount.Load())
	require.NoError(t, transaction.Finalize(ctx), "Finalize 必须幂等")
	assert.Equal(t, int32(1), old.closeCount.Load())
	_ = pending.Close()
}

func TestClusterSwitcher_CompleteRetriesFinalizeFailure(t *testing.T) {
	ctx := context.Background()
	oldBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	old := &retryCloseProvider{DiscoveryProvider: oldBase}
	old.failFirst.Store(true)
	pending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	switcher := cluster.NewClusterSwitcher(old, "svc")
	require.NoError(t, switcher.Begin(ctx, pending))
	require.Error(t, switcher.Complete(ctx))
	require.NoError(t, switcher.Complete(ctx), "再次 Complete 必须重试 Finalize")
	assert.Equal(t, int32(2), old.closeCount.Load())
	_ = pending.Close()
}

func TestClusterSwitcher_FinalizeTimeoutDoesNotHoldOperationLock(t *testing.T) {
	release := make(chan struct{})
	oldBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	old := &retryCloseProvider{DiscoveryProvider: oldBase, block: release}
	pending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	switcher := cluster.NewClusterSwitcher(old, "svc")
	transaction := switcher.(cluster.ProviderSwitchTransaction)
	require.NoError(t, switcher.Begin(context.Background(), pending))
	require.NoError(t, transaction.Promote(context.Background()))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := transaction.Finalize(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	beginCtx, beginCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer beginCancel()
	err = switcher.Begin(beginCtx, cluster.NewLocalProvider(time.Second, time.Second, time.Second))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "尚未完成关闭", "Begin 应快速报告 retired，而不是等待 Close")
	close(release)
	require.NoError(t, transaction.Finalize(context.Background()))
	_ = pending.Close()
}

func TestClusterSwitcher_FinalizePrefersContextCloser(t *testing.T) {
	oldBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	old := &contextCloseProvider{DiscoveryProvider: oldBase}
	pending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	switcher := cluster.NewClusterSwitcher(old, "svc")
	transaction := switcher.(cluster.ProviderSwitchTransaction)
	require.NoError(t, switcher.Begin(context.Background(), pending))
	require.NoError(t, transaction.Promote(context.Background()))
	require.NoError(t, transaction.Finalize(context.Background()))
	assert.Zero(t, old.closeCount.Load())
	assert.Equal(t, int32(1), old.closeContextCount.Load())
	_ = pending.Close()
}

func TestClusterSwitcher_BeginMigratesOnlyScopedServiceNodes(t *testing.T) {
	ctx := context.Background()
	provA := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provB := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provA.Start()
	provB.Start()
	defer provA.Close()
	defer provB.Close()

	require.NoError(t, provA.Register(ctx, &cluster.NodeInfo{
		ID:           "mysvc-1",
		ServiceName:  "mysvc",
		DataCenterID: 1,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8080,
		Weight:       1,
	}))
	require.NoError(t, provA.Register(ctx, &cluster.NodeInfo{
		ID:           "othersvc-1",
		ServiceName:  "othersvc",
		DataCenterID: 1,
		MachineID:    2,
		Address:      "127.0.0.1",
		Port:         8081,
		Weight:       1,
	}))

	switcher := cluster.NewClusterSwitcher(provA, "mysvc")
	require.NoError(t, switcher.Begin(ctx, provB))

	nodes, err := provB.List(ctx, "mysvc")
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, "mysvc-1", nodes[0].ID)

	otherNodes, err := provB.List(ctx, "othersvc")
	require.NoError(t, err)
	assert.Empty(t, otherNodes)
}

func TestClusterSwitcher_ReconcilesChangesDuringMigration(t *testing.T) {
	ctx := context.Background()
	current := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	pending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	defer current.Close()
	defer pending.Close()

	switcher := cluster.NewClusterSwitcher(current, "svc")
	require.NoError(t, switcher.Begin(ctx, pending))
	t.Cleanup(func() { _ = switcher.Rollback(context.Background()) })

	node := &cluster.NodeInfo{
		ID:           "svc-live-1",
		ServiceName:  "svc",
		DataCenterID: 1,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8080,
		Weight:       1,
	}
	require.NoError(t, current.Register(ctx, node))
	require.Eventually(t, func() bool {
		nodes, err := pending.List(ctx, "svc", cluster.NodeStatusRunning)
		return err == nil && len(nodes) == 1 && nodes[0].ID == node.ID
	}, time.Second, 10*time.Millisecond, "迁移期间新节点未同步到 pending Provider")

	require.NoError(t, current.Deregister(ctx, node.ID))
	require.Eventually(t, func() bool {
		nodes, err := pending.List(ctx, "svc", cluster.NodeStatusRunning)
		return err == nil && len(nodes) == 0
	}, time.Second, 10*time.Millisecond, "已下线节点仍保留在 pending Provider 的运行集合")
}

func TestClusterSwitcher_RetriesTransientPendingFailure(t *testing.T) {
	ctx := context.Background()
	current := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	pendingBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	pending := &switcherTestProvider{DiscoveryProvider: pendingBase}
	pending.registerFailures.Store(1)
	defer current.Close()
	defer pendingBase.Close()

	node := &cluster.NodeInfo{
		ID:           "svc-retry-1",
		ServiceName:  "svc",
		DataCenterID: 1,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8080,
		Weight:       1,
	}
	require.NoError(t, current.Register(ctx, node))
	switcher := cluster.NewClusterSwitcher(current, "svc")
	require.NoError(t, switcher.Begin(ctx, pending))
	t.Cleanup(func() { _ = switcher.Rollback(context.Background()) })

	require.Eventually(t, func() bool {
		_, err := pending.Get(ctx, node.ID)
		return err == nil
	}, time.Second, 10*time.Millisecond, "pending Provider 临时失败后未自动重试")
}

func TestClusterSwitcher_BeginListFailureLeavesSwitcherRetryable(t *testing.T) {
	ctx := context.Background()
	currentBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	current := &switcherTestProvider{
		DiscoveryProvider: currentBase,
		listErr:           errors.New("模拟列表失败"),
	}
	firstPending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	secondPending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	defer currentBase.Close()
	defer firstPending.Close()
	defer secondPending.Close()

	switcher := cluster.NewClusterSwitcher(current, "svc")
	require.Error(t, switcher.Begin(ctx, firstPending))

	current.listErr = nil
	require.NoError(t, switcher.Begin(ctx, secondPending), "Begin 失败后 Switcher 应可重试")
	require.NoError(t, switcher.Rollback(ctx))
}

func TestClusterSwitcher_BeginWatchFailureLeavesSwitcherRetryable(t *testing.T) {
	ctx := context.Background()
	currentBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	current := &switcherTestProvider{
		DiscoveryProvider: currentBase,
		watchErr:          errors.New("模拟 Watch 失败"),
	}
	firstPending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	secondPending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	defer currentBase.Close()
	defer firstPending.Close()
	defer secondPending.Close()

	switcher := cluster.NewClusterSwitcher(current, "svc")
	require.Error(t, switcher.Begin(ctx, firstPending))

	current.watchErr = nil
	require.NoError(t, switcher.Begin(ctx, secondPending), "Watch 失败后 Switcher 应可重试")
	require.NoError(t, switcher.Rollback(ctx))
}

func TestClusterSwitcher_CompleteWaitsForInFlightReconcile(t *testing.T) {
	ctx := context.Background()
	current := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	pendingBase := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	pending := &switcherTestProvider{
		DiscoveryProvider: pendingBase,
		blockRegister:     true,
		registerStarted:   make(chan struct{}),
		registerExited:    make(chan struct{}),
	}
	defer current.Close()
	defer pendingBase.Close()

	switcher := cluster.NewClusterSwitcher(current, "svc")
	require.NoError(t, switcher.Begin(ctx, pending))
	require.NoError(t, current.Register(ctx, &cluster.NodeInfo{
		ID:           "svc-blocked-1",
		ServiceName:  "svc",
		DataCenterID: 1,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8080,
		Weight:       1,
	}))

	select {
	case <-pending.registerStarted:
	case <-time.After(time.Second):
		t.Fatal("未等到 pending Register 进入阻塞状态")
	}
	require.NoError(t, switcher.Complete(ctx))
	select {
	case <-pending.registerExited:
	default:
		t.Fatal("Complete 在在途对账调用退出前返回")
	}
	assert.Same(t, pending, switcher.Current())
}

func TestClusterSwitcher_BeginCopiesOnlyRunningNodes(t *testing.T) {
	ctx := context.Background()
	current := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	pending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	defer current.Close()
	defer pending.Close()

	node := &cluster.NodeInfo{
		ID:           "svc-offline-1",
		ServiceName:  "svc",
		DataCenterID: 1,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8080,
		Weight:       1,
	}
	require.NoError(t, current.Register(ctx, node))
	require.NoError(t, current.Deregister(ctx, node.ID))

	switcher := cluster.NewClusterSwitcher(current, "svc")
	require.NoError(t, switcher.Begin(ctx, pending))
	require.ErrorIs(t, func() error {
		_, err := pending.Get(ctx, node.ID)
		return err
	}(), cluster.ErrNodeNotFound)
	require.NoError(t, switcher.Rollback(ctx))
}

func TestClusterSwitcher_RollbackStopsFutureReconciliation(t *testing.T) {
	ctx := context.Background()
	current := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	pending := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	defer current.Close()
	defer pending.Close()

	switcher := cluster.NewClusterSwitcher(current, "svc")
	require.NoError(t, switcher.Begin(ctx, pending))
	require.NoError(t, switcher.Rollback(ctx))

	node := &cluster.NodeInfo{
		ID:           "svc-after-rollback-1",
		ServiceName:  "svc",
		DataCenterID: 1,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         8080,
		Weight:       1,
	}
	require.NoError(t, current.Register(ctx, node))
	require.Never(t, func() bool {
		_, err := pending.Get(ctx, node.ID)
		return err == nil
	}, 150*time.Millisecond, 10*time.Millisecond, "Rollback 后仍在向 pending Provider 对账")
}
