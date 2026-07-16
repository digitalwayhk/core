package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	reconcileInitialBackoff = 50 * time.Millisecond
	reconcileMaxBackoff     = 2 * time.Second
)

type providerMigration struct {
	ctx         context.Context
	cancel      context.CancelFunc
	watchCancel func()
	stopOnce    sync.Once
	refresh     chan struct{}
	done        chan struct{}
	current     DiscoveryProvider
	pending     DiscoveryProvider
}

// ProviderSwitchShutdown is an optional switcher capability used by service
// shutdown to abort an active migration and release its pending provider.
type ProviderSwitchShutdown interface {
	Shutdown(ctx context.Context, active DiscoveryProvider) error
}

// clusterSwitcher 负责在两个 DiscoveryProvider 之间完成可对账的迁移。
type clusterSwitcher struct {
	opMu               sync.Mutex
	mu                 sync.RWMutex
	current            DiscoveryProvider
	pending            DiscoveryProvider
	retired            DiscoveryProvider
	inProgress         bool
	switchedAt         time.Time
	serviceName        string
	migration          *providerMigration
	closeFlight        *providerCloseFlight
	pendingCloseFlight *providerCloseFlight
}

type providerCloseFlight struct {
	provider DiscoveryProvider
	done     chan struct{}
	err      error
}

// NewClusterSwitcher 创建按服务名隔离的 Provider 切换器。
func NewClusterSwitcher(initial DiscoveryProvider, serviceName string) ProviderSwitcher {
	return &clusterSwitcher{current: initial, serviceName: serviceName}
}

func (s *clusterSwitcher) Current() DiscoveryProvider {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

// Begin 启动迁移并持续将 current Provider 的运行节点对账到 pending Provider。
func (s *clusterSwitcher) Begin(ctx context.Context, to DiscoveryProvider) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	s.mu.RLock()
	if s.inProgress {
		s.mu.RUnlock()
		return ErrMigrationInProgress
	}
	current := s.current
	retired := s.retired
	s.mu.RUnlock()
	if retired != nil {
		return errors.New("cluster switcher: 旧 Provider 尚未完成关闭")
	}
	if current == nil {
		return ErrNotStarted
	}
	if to == nil {
		return errors.New("cluster switcher: pending Provider 不能为 nil")
	}

	migrationCtx, cancel := context.WithCancel(context.Background())
	migration := &providerMigration{
		ctx:     migrationCtx,
		cancel:  cancel,
		refresh: make(chan struct{}, 1),
		done:    make(chan struct{}),
		current: current,
		pending: to,
	}
	watchCancel, err := current.Watch(migrationCtx, s.serviceName, func(_ []*NodeInfo) {
		signalMigrationRefresh(migration)
	})
	if err != nil {
		cancel()
		return fmt.Errorf("cluster switcher: 订阅 current Provider 失败: %w", err)
	}
	migration.watchCancel = watchCancel

	nodes, err := current.List(ctx, s.serviceName, NodeStatusRunning)
	if err != nil {
		watchCancel()
		cancel()
		return fmt.Errorf("cluster switcher: 查询 current Provider 节点失败: %w", err)
	}
	initialErr := reconcileProviderSnapshot(ctx, to, s.serviceName, nodes)

	s.mu.Lock()
	s.pending = to
	s.inProgress = true
	s.migration = migration
	s.mu.Unlock()
	go s.runReconciler(migration)
	if initialErr != nil {
		logx.Debugw("cluster_reconcile_retry",
			logx.Field("provider", to.Name()),
			logx.Field("attempt", 1),
			logx.Field("error", initialErr),
		)
		signalMigrationRefresh(migration)
	}
	return nil
}

// Complete 保持 ProviderSwitcher 的兼容后置条件：提升 pending 后关闭旧 Provider。
func (s *clusterSwitcher) Complete(ctx context.Context) error {
	if err := s.Promote(ctx); err != nil {
		return err
	}
	return s.Finalize(ctx)
}

// Promote 停止对账 worker并提升 pending Provider，保留旧 Provider 供运行时同步。
func (s *clusterSwitcher) Promote(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	if s.pendingCloseFlight != nil {
		return errors.New("cluster switcher: pending Provider 关闭尚未完成")
	}

	s.mu.RLock()
	alreadyPromoted := !s.inProgress && s.pending == nil && s.retired != nil
	s.mu.RUnlock()
	if alreadyPromoted {
		return nil
	}

	migration, old, pending, err := s.migrationSnapshot()
	if err != nil {
		return err
	}
	if err := stopProviderMigration(ctx, migration); err != nil {
		return err
	}

	s.mu.Lock()
	if s.migration != migration {
		s.mu.Unlock()
		return errors.New("cluster switcher: 迁移代次已变更")
	}
	s.current = pending
	s.retired = old
	s.pending = nil
	s.inProgress = false
	s.migration = nil
	s.switchedAt = time.Now()
	s.mu.Unlock()

	return nil
}

// Finalize closes the provider retired by Promote. Close runs outside opMu so
// a blocked provider cannot deadlock later management operations. A failed
// close is not cached: the next call starts a fresh attempt.
func (s *clusterSwitcher) Finalize(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cluster switcher: 关闭旧 Provider 前 context 已结束: %w", err)
	}

	s.opMu.Lock()
	s.mu.RLock()
	retired := s.retired
	s.mu.RUnlock()
	if retired == nil {
		s.opMu.Unlock()
		return nil
	}
	flight := s.closeFlight
	if flight == nil {
		flight = &providerCloseFlight{provider: retired, done: make(chan struct{})}
		s.closeFlight = flight
		go s.closeRetiredProvider(flight)
	}
	s.opMu.Unlock()

	select {
	case <-ctx.Done():
		return fmt.Errorf("cluster switcher: 等待旧 Provider %s 关闭失败: %w", retired.Name(), ctx.Err())
	case <-flight.done:
	}

	s.opMu.Lock()
	defer s.opMu.Unlock()
	err := flight.err
	if s.closeFlight == flight {
		s.closeFlight = nil
		if err == nil {
			s.mu.Lock()
			if s.retired == flight.provider {
				s.retired = nil
			}
			s.mu.Unlock()
		}
	}
	if err != nil {
		return fmt.Errorf("cluster switcher: 关闭旧 Provider %s 失败: %w", retired.Name(), err)
	}
	return nil
}

func (s *clusterSwitcher) closeRetiredProvider(flight *providerCloseFlight) {
	if closer, ok := flight.provider.(ContextCloser); ok {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		flight.err = closer.CloseContext(closeCtx)
		cancel()
	} else {
		flight.err = flight.provider.Close()
	}
	close(flight.done)
}

// Rollback 停止对账 worker，保留 current Provider，并关闭 pending Provider。
func (s *clusterSwitcher) Rollback(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	migration, _, pending, err := s.migrationSnapshot()
	if err != nil {
		return err
	}
	if err := stopProviderMigration(ctx, migration); err != nil {
		return err
	}
	if err := s.closePendingProvider(ctx, pending); err != nil {
		return err
	}
	return s.clearMigration(migration)
}

// Shutdown releases providers still owned by the switcher. active is retained
// for ServiceContext to close in its established shutdown order.
func (s *clusterSwitcher) Shutdown(ctx context.Context, active DiscoveryProvider) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.RLock()
	migration := s.migration
	pending := s.pending
	retired := s.retired
	current := s.current
	s.mu.RUnlock()
	if migration != nil {
		if err := stopProviderMigration(ctx, migration); err != nil {
			return err
		}
	}

	closed := make(map[DiscoveryProvider]struct{}, 3)
	closeOwned := func(label string, provider DiscoveryProvider, flight **providerCloseFlight) error {
		if provider == nil || provider == active {
			return nil
		}
		if _, ok := closed[provider]; ok {
			return nil
		}
		if err := s.closeProvider(ctx, label, provider, flight); err != nil {
			return err
		}
		closed[provider] = struct{}{}
		return nil
	}
	if err := closeOwned("pending", pending, &s.pendingCloseFlight); err != nil {
		return err
	}
	if err := closeOwned("retired", retired, &s.closeFlight); err != nil {
		return err
	}
	if err := closeOwned("current", current, &s.pendingCloseFlight); err != nil {
		return err
	}

	s.mu.Lock()
	if s.migration == migration {
		s.pending = nil
		s.inProgress = false
		s.migration = nil
	}
	if s.retired == retired {
		s.retired = nil
	}
	if s.current == current {
		s.current = nil
	}
	s.mu.Unlock()
	return nil
}

func (s *clusterSwitcher) clearMigration(migration *providerMigration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.migration != migration {
		return errors.New("cluster switcher: 迁移代次已变更")
	}
	s.pending = nil
	s.inProgress = false
	s.migration = nil
	return nil
}

func (s *clusterSwitcher) closePendingProvider(ctx context.Context, pending DiscoveryProvider) error {
	return s.closeProvider(ctx, "pending", pending, &s.pendingCloseFlight)
}

func (s *clusterSwitcher) closeProvider(
	ctx context.Context,
	label string,
	provider DiscoveryProvider,
	flightSlot **providerCloseFlight,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cluster switcher: 关闭 %s Provider %s 前 context 已结束: %w", label, provider.Name(), err)
	}
	if closer, ok := provider.(ContextCloser); ok {
		if err := closer.CloseContext(ctx); err != nil {
			return fmt.Errorf("cluster switcher: 关闭 %s Provider %s 失败: %w", label, provider.Name(), err)
		}
		return nil
	}

	flight := *flightSlot
	if flight == nil {
		flight = &providerCloseFlight{provider: provider, done: make(chan struct{})}
		*flightSlot = flight
		go func() {
			flight.err = provider.Close()
			close(flight.done)
		}()
	} else if flight.provider != provider {
		return fmt.Errorf("cluster switcher: %s Provider %s 的关闭仍在进行", label, flight.provider.Name())
	}
	select {
	case <-ctx.Done():
		return fmt.Errorf("cluster switcher: 等待 %s Provider %s 关闭失败: %w", label, provider.Name(), ctx.Err())
	case <-flight.done:
	}
	err := flight.err
	if *flightSlot == flight {
		*flightSlot = nil
	}
	if err != nil {
		return fmt.Errorf("cluster switcher: 关闭 %s Provider %s 失败: %w", label, provider.Name(), err)
	}
	return nil
}

func (s *clusterSwitcher) migrationSnapshot() (
	*providerMigration,
	DiscoveryProvider,
	DiscoveryProvider,
	error,
) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.inProgress || s.pending == nil || s.migration == nil {
		return nil, nil, nil, errors.New("cluster switcher: 当前没有进行中的迁移")
	}
	return s.migration, s.current, s.pending, nil
}

func (s *clusterSwitcher) runReconciler(migration *providerMigration) {
	defer close(migration.done)
	for {
		select {
		case <-migration.ctx.Done():
			return
		case <-migration.refresh:
		}

		backoff := reconcileInitialBackoff
		for {
			nodes, err := migration.current.List(
				migration.ctx,
				s.serviceName,
				NodeStatusRunning,
			)
			if err == nil {
				err = reconcileProviderSnapshot(
					migration.ctx,
					migration.pending,
					s.serviceName,
					nodes,
				)
			}
			if err == nil {
				break
			}
			logx.Debugw("cluster_reconcile_retry",
				logx.Field("provider", migration.pending.Name()),
				logx.Field("error", err),
			)

			timer := time.NewTimer(backoff)
			select {
			case <-migration.ctx.Done():
				timer.Stop()
				return
			case <-migration.refresh:
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
			}
			backoff *= 2
			if backoff > reconcileMaxBackoff {
				backoff = reconcileMaxBackoff
			}
		}
	}
}

func reconcileProviderSnapshot(
	ctx context.Context,
	pending DiscoveryProvider,
	serviceName string,
	nodes []*NodeInfo,
) error {
	desired := make(map[string]*NodeInfo, len(nodes))
	var result error
	for _, node := range nodes {
		if node == nil || node.Status != NodeStatusRunning {
			continue
		}
		cloned := cloneNodeInfo(node)
		desired[cloned.ID] = cloned
		if err := pending.Register(ctx, cloned); err != nil {
			result = errors.Join(result, fmt.Errorf("注册节点 %s 失败: %w", cloned.ID, err))
		}
	}

	actual, err := pending.List(ctx, serviceName)
	if err != nil {
		return errors.Join(result, fmt.Errorf("查询 pending Provider 节点失败: %w", err))
	}
	for _, node := range actual {
		if node == nil || node.Status == NodeStatusOffline {
			continue
		}
		if _, ok := desired[node.ID]; ok {
			continue
		}
		if err := pending.Deregister(ctx, node.ID); err != nil && err != ErrNodeNotFound {
			result = errors.Join(result, fmt.Errorf("注销节点 %s 失败: %w", node.ID, err))
		}
	}
	return result
}

func cloneNodeInfo(node *NodeInfo) *NodeInfo {
	cloned := *node
	if node.Metadata != nil {
		cloned.Metadata = make(map[string]string, len(node.Metadata))
		for key, value := range node.Metadata {
			cloned.Metadata[key] = value
		}
	}
	return &cloned
}

func signalMigrationRefresh(migration *providerMigration) {
	select {
	case <-migration.ctx.Done():
		return
	default:
	}
	select {
	case migration.refresh <- struct{}{}:
	default:
	}
}

func stopProviderMigration(ctx context.Context, migration *providerMigration) error {
	migration.stopOnce.Do(func() {
		if migration.watchCancel != nil {
			migration.watchCancel()
		}
		migration.cancel()
	})
	select {
	case <-migration.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cluster switcher: 等待对账 worker 退出失败: %w", ctx.Err())
	}
}
