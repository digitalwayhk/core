// 本文件通过真实三进程、mTLS 调用及独立网络代理验证注册失效恢复。
package shopmicroservices_test

import (
	"context"
	"fmt"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestStableThreeProcessDiscoveryRecovery 以真实业务调用确认恢复，检查健康接口不足以代表服务就绪。
func TestStableThreeProcessDiscoveryRecovery(t *testing.T) {
	testStableThreeProcessDiscoveryRecovery(t, "redis")
}

// TestStableThreeProcessEtcdDiscoveryRecovery 验证 etcd 断网超过租约后真实 mTLS 调用恢复。
func TestStableThreeProcessEtcdDiscoveryRecovery(t *testing.T) {
	testStableThreeProcessDiscoveryRecovery(t, "etcd")
}

func testStableThreeProcessDiscoveryRecovery(t *testing.T, provider string) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("需要独立测试 Redis")
	}
	ctx := context.Background()
	ns := fmt.Sprintf("stable-06-%d", time.Now().UnixNano())
	prefix := "core:test:" + ns
	root := t.TempDir()
	gc := &config.IDGeneratorConfig{Provider: "redis", Namespace: ns, Epoch: "epoch-a", Providers: config.ClusterProviderConfig{Redis: config.RedisProviderConfig{Addr: addr}}}
	a, _, err := identity.BuildAuthority(gc)
	require.NoError(t, err)
	defer a.Close()
	require.NoError(t, a.Initialize(ctx))
	for _, name := range []string{"shop-user", "shop-supplier", "shop-order"} {
		require.NoError(t, identity.Create(filepath.Join(root, name, "identity.json"), name, name+"-a"))
	}
	target := addr
	if provider == "etcd" {
		target = os.Getenv("CORE_TEST_ETCD_ADDR")
		if target == "" {
			t.Skip("需要独立测试 etcd")
		}
	}
	proxy := newDiscoveryProxy(t, target)
	defer proxy.close()
	pki := integration.NewGRPCTestPKI(t, "shop-user", "shop-supplier", "shop-order")
	env := map[string]string{"SHOP_REDIS_ADDR": proxy.listener.Addr().String(), "SHOP_IDENTITY_ROOT": root, "SHOP_GENERATOR_NAMESPACE": ns, "SHOP_GENERATOR_EPOCH": "epoch-a", "SHOP_GENERATOR_REDIS_ADDR": addr}
	if provider == "etcd" {
		env["SHOP_REDIS_ADDR"] = addr
		env["SHOP_ETCD_DISCOVERY_ENDPOINT"] = proxy.listener.Addr().String()
	}
	user, supplier, order := startShopProcessesWithEnvironment(t, pki, prefix, env)
	defer user.Stop()
	defer supplier.Stop()
	defer order.Stop()
	processes := []*integration.Suite{user, supplier, order}
	for _, p := range processes {
		waitProcessReady(t, p, "/api/ready", processes...)
	}
	waitProcessReady(t, user, "/api/shop-user/getproducts", processes...)
	var registry cluster.DiscoveryProvider
	if provider == "etcd" {
		registry, err = cluster.NewEtcdProviderWithPrefix([]string{target}, 5*time.Second, prefix+":discovery")
	} else {
		registry, err = cluster.NewRedisProvider(addr, 0, prefix+":discovery", 5*time.Second)
	}
	require.NoError(t, err)
	defer registry.Close()
	before, err := registry.List(ctx, "", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, before, 3)
	raw := redis.NewClient(&redis.Options{Addr: addr})
	defer raw.Close()
	var etcd *clientv3.Client
	if provider == "etcd" {
		etcd, err = clientv3.New(clientv3.Config{Endpoints: []string{target}, DialTimeout: time.Second})
		require.NoError(t, err)
		defer etcd.Close()
	}
	// 不发送发现事件，直接删本轮节点键，必须由心跳/周期对账恢复。
	for _, n := range before {
		if etcd != nil {
			_, err = etcd.Delete(ctx, prefix+":discovery/"+n.ServiceName+"/"+n.ID)
			require.NoError(t, err)
		} else {
			require.NoError(t, raw.Del(ctx, prefix+":discovery:nodes:"+n.ServiceName+":"+n.ID).Err())
		}
	}
	require.Eventually(t, func() bool {
		nodes, e := registry.List(ctx, "", cluster.NodeStatusRunning)
		return e == nil && len(nodes) == 3
	}, 10*time.Second, 50*time.Millisecond)
	proxy.setDown(true)
	require.Eventually(t, func() bool { return probeHTTP(user.BaseURL+"/api/ready") == http.StatusServiceUnavailable }, 8*time.Second, 50*time.Millisecond)
	require.Equal(t, http.StatusOK, probeHTTP(user.BaseURL+"/api/health"))
	// 大于实际发现 TTL，确保不是仍靠旧 lease 工作。
	time.Sleep(6 * time.Second)
	require.Eventually(t, func() bool {
		nodes, e := registry.List(ctx, "", cluster.NodeStatusRunning)
		return e == nil && len(nodes) == 0
	}, 5*time.Second, 50*time.Millisecond)
	started := time.Now()
	proxy.setDown(false)
	for _, p := range processes {
		require.Eventually(t, func() bool { return probeHTTP(p.BaseURL+"/api/ready") == http.StatusOK }, 30*time.Second, 50*time.Millisecond)
	}
	waitProcessReady(t, user, "/api/shop-user/getproducts", processes...)
	after, err := registry.List(ctx, "", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, after, 3)
	for _, old := range before {
		found := false
		for _, n := range after {
			if n.ID == old.ID {
				found = true
				require.Equal(t, old.ServiceInstanceID, n.ServiceInstanceID)
				require.Equal(t, old.MachineID, n.MachineID)
				require.Equal(t, old.StableBootID, n.StableBootID)
			}
		}
		require.True(t, found)
	}
	t.Logf("真实三进程（%s）：断连超过 TTL 后恢复耗时 %s；实例/编号/Boot 不变，mTLS 调用成功", provider, time.Since(started))
	// 使用原配置、业务目录及身份卷正常重启。
	order.StopProcess()
	require.NoError(t, order.Restart())
	waitProcessReady(t, order, "/api/ready", processes...)
	nodes, err := registry.List(ctx, "shop-order", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	for _, old := range before {
		if old.ServiceName == "shop-order" {
			require.Equal(t, old.ID, nodes[0].ID)
			require.Equal(t, old.MachineID, nodes[0].MachineID)
			require.NotEqual(t, old.StableBootID, nodes[0].StableBootID)
		}
	}
	waitProcessReady(t, user, "/api/shop-user/getproducts", processes...)
	order.StopProcess()
	require.Eventually(t, func() bool {
		n, e := registry.List(ctx, "shop-order", cluster.NodeStatusRunning)
		return e == nil && len(n) == 0
	}, 5*time.Second, 50*time.Millisecond)
	time.Sleep(1200 * time.Millisecond)
	n, err := registry.List(ctx, "shop-order", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Empty(t, n, "退出后不能重新注册")
}

func probeHTTP(url string) int {
	c := http.Client{Timeout: time.Second}
	r, e := c.Get(url)
	if e != nil {
		return 0
	}
	defer r.Body.Close()
	_, _ = io.Copy(io.Discard, r.Body)
	return r.StatusCode
}

type discoveryProxy struct {
	listener    net.Listener
	target      string
	mu          sync.Mutex
	down        bool
	connections map[net.Conn]struct{}
	wg          sync.WaitGroup
}

func newDiscoveryProxy(t *testing.T, target string) *discoveryProxy {
	t.Helper()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, e)
	p := &discoveryProxy{listener: ln, target: target, connections: map[net.Conn]struct{}{}}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			p.mu.Lock()
			if p.down {
				p.mu.Unlock()
				_ = c.Close()
				continue
			}
			p.connections[c] = struct{}{}
			p.wg.Add(1)
			p.mu.Unlock()
			go p.forward(c)
		}
	}()
	return p
}
func (p *discoveryProxy) forward(c net.Conn) {
	defer p.wg.Done()
	defer func() { _ = c.Close(); p.mu.Lock(); delete(p.connections, c); p.mu.Unlock() }()
	b, e := net.DialTimeout("tcp", p.target, time.Second)
	if e != nil {
		return
	}
	defer b.Close()
	p.mu.Lock()
	if p.down {
		p.mu.Unlock()
		return
	}
	p.connections[b] = struct{}{}
	p.mu.Unlock()
	defer func() { p.mu.Lock(); delete(p.connections, b); p.mu.Unlock() }()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(b, c); _ = b.Close(); close(done) }()
	_, _ = io.Copy(c, b)
	_ = c.Close()
	<-done
}
func (p *discoveryProxy) setDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
	if down {
		for c := range p.connections {
			_ = c.Close()
		}
	}
}
func (p *discoveryProxy) close() { _ = p.listener.Close(); p.setDown(true); p.wg.Wait() }
