// 本文件验证持久账本跨客户端恢复及原子比较写入。
package identity

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestFileAuthorityPersistsAcrossClients 验证编号权威不依赖服务进程内存。
func TestFileAuthorityPersistsAcrossClients(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authority.json")
	ctx := context.Background()
	a := NewAuthority(NewFileStore(path), "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "orders-a", 1, 0, 31, true)
	require.NoError(t, err)
	require.NoError(t, a.Close())
	other := NewAuthority(NewFileStore(path), "epoch-a")
	restored, err := other.Get(ctx, b.Key)
	require.NoError(t, err)
	require.Equal(t, b, restored)
	_, err = other.Bind(ctx, "other", "other-a", 0, 10, 31, false)
	require.Error(t, err)
}

// TestExternalAuthoritySurvivesDiscoveryClose 验证发现关闭不会释放编号客户端或绑定。
func TestExternalAuthoritySurvivesDiscoveryClose(t *testing.T) {
	for _, provider := range []string{"redis", "etcd"} {
		t.Run(provider, func(t *testing.T) {
			gc := &config.IDGeneratorConfig{Provider: provider, Namespace: fmt.Sprintf("test-%d", time.Now().UnixNano()), Epoch: "epoch-a"}
			var discovery cluster.DiscoveryProvider
			var err error
			if provider == "redis" {
				addr := os.Getenv("CORE_TEST_REDIS_ADDR")
				if addr == "" {
					t.Skip("需要独立 Redis")
				}
				gc.Providers.Redis.Addr = addr
				discovery, err = cluster.NewRedisProvider(addr, 0, gc.Namespace, time.Second)
			} else {
				addr := os.Getenv("CORE_TEST_ETCD_ADDR")
				if addr == "" {
					t.Skip("需要独立 etcd")
				}
				gc.Providers.Etcd.Endpoints = []string{addr}
				discovery, err = cluster.NewEtcdProviderWithPrefix([]string{addr}, time.Second, "/test/"+gc.Namespace)
			}
			require.NoError(t, err)
			defer discovery.Close()
			a, _, err := BuildAuthority(gc)
			require.NoError(t, err)
			defer a.Close()
			ctx := context.Background()
			require.NoError(t, a.Initialize(ctx))
			b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, true)
			require.NoError(t, err)
			require.NoError(t, discovery.Close())
			current, err := a.Get(ctx, b.Key)
			require.NoError(t, err)
			require.Equal(t, b, current)
			other, _, err := BuildAuthority(gc)
			require.NoError(t, err)
			defer other.Close()
			current, err = other.Get(ctx, b.Key)
			require.NoError(t, err)
			require.Equal(t, b, current)
		})
	}
}
