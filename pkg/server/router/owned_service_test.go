// 本文件以真实Redis和健康节点快照验证owner精确选择；独立进程另做集成验收。
package router

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

func TestCallServiceOwnedFollowsLeaseWhileBothProcessesHealthy(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	ctx := context.Background()
	service := fmt.Sprintf("owner-service-%d", time.Now().UnixNano())
	p := mq.NewRedisStreamProviderWithConfig(config.RedisStreamMQConfig{Addr: addr, Prefix: service, CommandPoolSize: 4, BlockingReadPoolSize: 4})
	require.NoError(t, p.Connect(ctx))
	defer p.Close()
	manager := mq.NewManager()
	manager.Register(p)
	require.NoError(t, manager.SetCurrent(p.Name()))
	defer manager.Close()
	m := ownership.Manifest{Scope: service, Service: service, Group: service, Selector: "key-v1", Keys: []string{"A"}, Subjects: []string{"one"}}.Normalize()
	require.NoError(t, manager.ProvisionOwnership(ctx, m, 0))
	store, err := manager.OwnershipStore(ctx)
	require.NoError(t, err)
	provider := cluster.NewLocalProvider(time.Minute, time.Minute, time.Minute)
	provider.Start()
	defer provider.Close()
	for i, id := range []string{"a", "b"} {
		require.NoError(t, provider.Register(ctx, &cluster.NodeInfo{ID: service + id, ServiceName: service, ServiceInstanceID: id, Address: "healthy-" + id, Port: 8080, DataCenterID: 1, MachineID: int64(i + 1)}))
	}
	transport := &resolverTestTransport{}
	sc := &ServiceContext{Service: &types.Service{Name: "caller"}, Config: config.NewServiceDefaultConfig("caller", 1), MQManager: manager, ServiceResolver: NewServiceResolver(provider, func(string) *ServiceContext { return nil }), TransportSelector: &resolverTestSelector{transport: transport}}
	defer sc.ServiceResolver.Close()
	a, err := store.Acquire(ctx, ownership.Credential{Scope: m.Scope, Key: "A", Service: service, InstanceID: "a", SessionID: "boot-a"}, m.Timing.Lease)
	require.NoError(t, err)
	require.NoError(t, store.Ready(ctx, a))
	payload := &types.PayLoad{TargetService: service, TargetPath: "/api/read"}
	_, err = sc.CallServiceOwned(ctx, payload, m.Scope, "A")
	require.NoError(t, err)
	require.Equal(t, "healthy-a", transport.targetAddress)
	require.NoError(t, store.Release(ctx, a))
	b, err := store.Acquire(ctx, ownership.Credential{Scope: m.Scope, Key: "A", Service: service, InstanceID: "b", SessionID: "boot-b"}, m.Timing.Lease)
	require.NoError(t, err)
	_, err = sc.CallServiceOwned(ctx, payload, m.Scope, "A")
	require.ErrorIs(t, err, ownership.ErrNotReady, "未完成恢复不能公布新owner")
	require.NoError(t, store.Ready(ctx, b))
	_, err = sc.CallServiceOwned(ctx, payload, m.Scope, "A")
	require.NoError(t, err)
	require.Equal(t, "healthy-b", transport.targetAddress)
	require.Empty(t, payload.TargetAddress, "不能修改调用方原始payload")
}
