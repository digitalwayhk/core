// 本文件验证稳定注册的机器编号解耦、丢失恢复和旧启动世代拒绝。
package cluster

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testOwnedRegistration(t *testing.T, p DiscoveryProvider) {
	t.Helper()
	ctx := context.Background()
	owned, ok := p.(OwnedRegistry)
	require.True(t, ok)
	n := &NodeInfo{ID: "stable-orders-a", ServiceName: "orders", ServiceInstanceID: "a", MachineID: 1, DataCenterID: 1, Address: "127.0.0.1", Port: 18000}
	one := RegistrationOwner{BootID: "boot-a", Epoch: 1}
	require.NoError(t, owned.AcquireRegistration(ctx, n, one))
	require.NoError(t, owned.ReleaseRegistration(ctx, n, one))
	_, err := p.Get(ctx, n.ID)
	require.ErrorIs(t, err, ErrNodeNotFound)
	require.NoError(t, owned.RenewRegistration(ctx, n, one), "节点消失后用本地完整信息恢复，不依赖旧节点键")
	two := RegistrationOwner{BootID: "boot-b", Epoch: 2}
	n.Address = "127.0.0.2"
	require.NoError(t, owned.AcquireRegistration(ctx, n, two))
	require.ErrorIs(t, owned.RenewRegistration(ctx, n, one), ErrRegistrationFenced)
	require.ErrorIs(t, owned.ReleaseRegistration(ctx, n, one), ErrRegistrationFenced)
	current, err := p.Get(ctx, n.ID)
	require.NoError(t, err)
	require.Equal(t, n.Address, current.Address)
	legacy := cloneNode(n)
	legacy.StableBootID = ""
	legacy.StableBootEpoch = 0
	legacy.Address = "legacy-overwrite"
	require.ErrorIs(t, p.Register(ctx, legacy), ErrRegistrationFenced, "旧注册入口不能绕过稳定 owner")
	require.ErrorIs(t, p.Deregister(ctx, n.ID), ErrRegistrationFenced, "旧注销入口不能删除稳定 owner")
	other := cloneNode(n)
	other.ID = "stable-orders-b"
	other.ServiceInstanceID = "b"
	require.NoError(t, owned.AcquireRegistration(ctx, other, RegistrationOwner{BootID: "boot-c", Epoch: 1}), "注册不检查 MachineID 槽位，编号权威单独负责")
}

// TestOwnedRegistrationLocal 验证同进程稳定注册契约。
func TestOwnedRegistrationLocal(t *testing.T) {
	p := NewLocalProvider(time.Second, time.Second, time.Second)
	defer p.Close()
	testOwnedRegistration(t, p)
}

// TestOwnedRegistrationRedis 验证真实 Redis 原子注册契约，不触碰共享数据。
func TestOwnedRegistrationRedis(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("需要独立 Redis")
	}
	p, err := NewRedisProvider(addr, 0, fmt.Sprintf("core:test:owned:%d", time.Now().UnixNano()), time.Second)
	require.NoError(t, err)
	defer p.Close()
	testOwnedRegistration(t, p)
}

// TestOwnedRegistrationEtcd 验证真实 etcd 事务拒绝旧启动和 legacy 旁路。
func TestOwnedRegistrationEtcd(t *testing.T) {
	addr := os.Getenv("CORE_TEST_ETCD_ADDR")
	if addr == "" {
		t.Skip("需要独立 etcd")
	}
	p, err := NewEtcdProviderWithPrefix([]string{addr}, time.Second, fmt.Sprintf("/core/test/owned/%d", time.Now().UnixNano()))
	require.NoError(t, err)
	defer p.Close()
	testOwnedRegistration(t, p)
}
