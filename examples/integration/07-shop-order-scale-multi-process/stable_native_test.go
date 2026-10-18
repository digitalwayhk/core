// 本文件在独立进程中验证 07 稳定双副本及原卷重启，不复用部署 Compose 或已有数据。
package shoporderscalemultiprocess

import (
	"context"
	"fmt"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestStableNativeOrderReplicas(t *testing.T) {
	a, ns, processes := startStableNativeProcesses(t, 2)
	redisAddr := os.Getenv("CORE_TEST_REDIS_ADDR")
	mysqlAddr := os.Getenv("CORE_TEST_STABLE_MYSQL_ADDR")
	registry, err := cluster.NewRedisProvider(redisAddr, 0, ns, 5*time.Second)
	require.NoError(t, err)
	defer registry.Close()
	before, err := registry.List(context.Background(), "shop-order", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, before, 2)
	require.NotEqual(t, before[0].ServiceInstanceID, before[1].ServiceInstanceID)
	require.NotEqual(t, before[0].MachineID, before[1].MachineID)
	admin := prepareDockerAdminFixture(t, processes[1])
	supplier := prepareDockerSupplierFixture(t, processes[1], admin)
	user := processes[0]
	token := user.TokenFor(t, "720001", 0)
	created := createDockerBuyerOrderWithRequest(t, user, token, supplier.ProductID, "stable-native-request")
	retried := createDockerBuyerOrderWithRequest(t, user, token, supplier.ProductID, "stable-native-request")
	require.Equal(t, created.OrderID, retried.OrderID)
	waitDockerOrderVisible(t, user, token, created.OrderID)
	processes[2].StopProcess()
	require.NoError(t, processes[2].Restart())
	require.Eventually(t, func() bool { return nativeProbe(processes[2].BaseURL+"/api/ready") == http.StatusOK }, 30*time.Second, 100*time.Millisecond)
	after, err := registry.List(context.Background(), "shop-order", cluster.NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, after, 2)
	for _, old := range before {
		found := false
		for _, n := range after {
			if n.ID == old.ID {
				found = true
				require.Equal(t, old.ServiceInstanceID, n.ServiceInstanceID)
				require.Equal(t, old.MachineID, n.MachineID)
			}
		}
		require.True(t, found)
	}
	waitDockerOrderVisible(t, user, token, created.OrderID)
	retried = createDockerBuyerOrderWithRequest(t, user, token, supplier.ProductID, "stable-native-request")
	require.Equal(t, created.OrderID, retried.OrderID)
	t.Logf("07 双 order 副本：编号独立；原卷重启身份不变；共享权威订单及幂等结果保持，pending 路径保留")
	t.Run("真实积压硬崩溃恢复", func(t *testing.T) {
		verifyStablePendingCrashRecovery(t, a, mysqlAddr, "core_"+ns, user, token, supplier.ProductID, processes[2:])
	})
}
func freeNativePort(t *testing.T) int {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, e)
	p := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return p
}
func nativeProbe(url string) int {
	c := http.Client{Timeout: time.Second}
	r, e := c.Get(url)
	if e != nil {
		return 0
	}
	defer r.Body.Close()
	return r.StatusCode
}

// startStableNativeProcesses 复用稳定身份、独占卷及真实多进程夹具。
func startStableNativeProcesses(t *testing.T, orderReplicas int) (*identity.Authority, string, []*integration.Suite) {
	t.Helper()
	redisAddr := os.Getenv("CORE_TEST_REDIS_ADDR")
	mysqlAddr := os.Getenv("CORE_TEST_STABLE_MYSQL_ADDR")
	if redisAddr == "" || mysqlAddr == "" {
		t.Skip("需要专用 Redis 和 CORE_TEST_STABLE_MYSQL_ADDR")
	}
	host, port, err := net.SplitHostPort(mysqlAddr)
	require.NoError(t, err)
	ns := fmt.Sprintf("stable-07-%d", time.Now().UnixNano())
	root := nativeFixtureStateRoot(t)
	gc := &config.IDGeneratorConfig{Provider: "redis", Namespace: ns, Epoch: "epoch-a", Providers: config.ClusterProviderConfig{Redis: config.RedisProviderConfig{Addr: redisAddr}}}
	a, _, err := identity.BuildAuthority(gc)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, a.Close()) })
	require.NoError(t, a.Initialize(context.Background()))
	pki := integration.NewGRPCTestPKI(t, "shop-user", "shop-supplier", "shop-order")
	var processes []*integration.Suite
	t.Cleanup(func() {
		for _, p := range processes {
			if t.Failed() {
				p.PrintLog()
			}
			p.Stop()
		}
	})
	names := []string{"shop-user", "shop-supplier", "shop-order", "shop-order"}
	binaries := []string{"user", "supplier", "order", "order"}
	for i, name := range names[:2+orderReplicas] {
		replica := fmt.Sprintf("replica-%d", i)
		stateRoot := filepath.Join(root, replica, "identity")
		require.NoError(t, identity.Create(filepath.Join(stateRoot, name, "identity.json"), name, replica))
		cert := pki.Services[name]
		env := map[string]string{
			"SHOP_REDIS_ADDR": redisAddr, "SHOP_REDIS_DISCOVERY_PREFIX": ns,
			"SHOP_IDENTITY_ROOT": stateRoot, "SHOP_GENERATOR_REDIS_ADDR": redisAddr, "SHOP_GENERATOR_NAMESPACE": ns, "SHOP_GENERATOR_EPOCH": "epoch-a",
			"SHOP_GRPC_SECURITY_MODE": "mtls", "SHOP_GRPC_CA_FILE": pki.CAFile, "SHOP_GRPC_CERT_FILE": cert.CertFile, "SHOP_GRPC_KEY_FILE": cert.KeyFile, "SHOP_GRPC_SERVER_NAME": "{service}", "SHOP_ADVERTISE_ADDRESS": "127.0.0.1",
			"SHOP_METRICS_HOST": "127.0.0.1", "SHOP_METRICS_PORT": strconv.Itoa(freeNativePort(t)),
			"SHOP_LOCAL_PENDING_DIR":       filepath.Join(root, replica, "pending"),
			"SHOP_ORDER_REMOTE_MYSQL_HOST": host, "SHOP_ORDER_REMOTE_MYSQL_PORT": port, "SHOP_ORDER_REMOTE_MYSQL_USER": "root", "SHOP_ORDER_REMOTE_MYSQL_PASSWORD": os.Getenv("CORE_TEST_STABLE_MYSQL_PASSWORD"), "SHOP_ORDER_REMOTE_MYSQL_DATABASE": "core_" + ns,
		}
		count := i + 2
		if i >= 2 {
			count = 4
		}
		p, e := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/07-shop-order-scale/main/" + binaries[i], BinaryName: replica, ServiceCount: count, ServiceIndex: count - 1, HTTPServiceOffsets: []int{0, count - 1}, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: env, DisableRace: !integration.IsRaceRun()})
		require.NoError(t, e)
		processes = append(processes, p)
		require.Eventually(t, func() bool { return nativeProbe(p.BaseURL+"/api/ready") == http.StatusOK }, 30*time.Second, 100*time.Millisecond, p.BaseURL)
	}
	return a, ns, processes
}

func nativeFixtureStateRoot(t *testing.T) string {
	t.Helper()
	if os.Getenv("CORE_TEST_ORDER_LOAD") != "1" {
		return t.TempDir()
	}
	evidence := os.Getenv("CORE_TEST_LOAD_EVIDENCE_DIR")
	require.True(t, filepath.IsAbs(evidence), "显式压测必须指定绝对证据目录")
	directory, err := os.MkdirTemp(evidence, "state-")
	require.NoError(t, err)
	t.Logf("压测本地状态保留于 %s", directory)
	return directory
}
