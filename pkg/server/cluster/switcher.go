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
	refresh     chan struct{}
	done        chan struct{}
	current     DiscoveryProvider
	pending     DiscoveryProvider
}

// clusterSwitcher 负责在两个 DiscoveryProvider 之间完成可对账的迁移。
type clusterSwitcher struct {
	opMu        sync.Mutex
	mu          sync.RWMutex
	current     DiscoveryProvider
	pending     DiscoveryProvider
	inProgress  bool
	switchedAt  time.Time
	serviceName string
	migration   *providerMigration
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
	s.mu.RUnlock()
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
		logx.Errorf("cluster switcher: 首次对账到 %s 失败，将自动重试: %v", to.Name(), initialErr)
		signalMigrationRefresh(migration)
	}
	return nil
}

// Complete 停止对账 worker，提升 pending Provider，并关闭旧 Provider。
func (s *clusterSwitcher) Complete(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

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
	s.pending = nil
	s.inProgress = false
	s.migration = nil
	s.switchedAt = time.Now()
	s.mu.Unlock()

	if err := old.Close(); err != nil {
		return fmt.Errorf("cluster switcher: 关闭旧 Provider %s 失败: %w", old.Name(), err)
	}
	return nil
}

// Rollback 停止对账 worker，保留 current Provider，并关闭 pending Provider。
func (s *clusterSwitcher) Rollback(ctx context.Context) error {
	s.opMu.Lock()
	defer s.opMu.Unlock()

	migration, _, pending, err := s.migrationSnapshot()
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
	s.pending = nil
	s.inProgress = false
	s.migration = nil
	s.mu.Unlock()

	if err := pending.Close(); err != nil {
		return fmt.Errorf("cluster switcher: 关闭 pending Provider %s 失败: %w", pending.Name(), err)
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
			logx.Errorf("cluster switcher: 对账到 %s 失败，正在重试: %v", migration.pending.Name(), err)

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
	migration.watchCancel()
	migration.cancel()
	select {
	case <-migration.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cluster switcher: 等待对账 worker 退出失败: %w", ctx.Err())
	}
}
