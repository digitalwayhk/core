// 本文件只复用测试资源与数据准备，计时前停止 Core 应用并启动独立原生三进程。
package shoporderscalemultiprocess

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

func replaceWithNativeGoZero(t *testing.T, original []*integration.Suite, evidence string) []*integration.Suite {
	t.Helper()
	configs := make([]string, 3)
	for i, p := range original {
		role := []string{"user", "supplier", "order"}[i]
		configs[i] = findReferenceFile(t, p.RootDir, func(path string, data []byte) bool {
			if !strings.HasSuffix(path, ".json") {
				return false
			}
			var c struct{ Name string }
			return json.Unmarshal(data, &c) == nil && c.Name == "shop-"+role
		})
		p.StopProcess()
		if b, e := os.ReadFile(filepath.Join(p.RootDir, "service.log")); e == nil {
			require.NoError(t, os.WriteFile(filepath.Join(evidence, fmt.Sprintf("bootstrap-process-%d.log", i)), b, 0600))
		}
	}
	supplierDB := findReferenceFile(t, original[1].RootDir, func(path string, _ []byte) bool { return strings.HasSuffix(path, "shop-supplier-scale.ldb") })
	processes := make([]*integration.Suite, 3)
	t.Cleanup(func() {
		for i, p := range processes {
			if p != nil {
				p.StopProcess()
				data, e := os.ReadFile(filepath.Join(p.RootDir, "service.log"))
				require.NoError(t, e)
				require.NoError(t, os.WriteFile(filepath.Join(evidence, fmt.Sprintf("process-%d.log", i)), data, 0600))
				p.Stop()
			}
		}
	})
	for _, i := range []int{2, 1, 0} {
		role := []string{"user", "supplier", "order"}[i]
		env := map[string]string{}
		for k, v := range original[i].Environment {
			env[k] = v
		}
		env["REFERENCE_ROLE"] = role
		env["REFERENCE_CONFIG_FILE"] = configs[i]
		env["REFERENCE_SUPPLIER_DB"] = supplierDB
		env["SHOP_LOCAL_PENDING_DIR"] = filepath.Join(evidence, "native-pending")
		if i == 0 {
			env["REFERENCE_SUPPLIER_RPC"] = fmt.Sprintf("127.0.0.1:%d", processes[1].GRPCBasePort)
			env["REFERENCE_ORDER_RPC"] = fmt.Sprintf("127.0.0.1:%d", processes[2].GRPCBasePort)
		}
		grpcCount := 1
		if i == 0 {
			grpcCount = 0
		}
		p, e := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/07-shop-order-scale/go-zero-reference/main", BinaryName: "native-" + role, ServiceCount: 1, GRPCServiceCount: grpcCount, Environment: env, DisableRace: !integration.IsRaceRun()})
		require.NoError(t, e)
		processes[i] = p
		require.Eventually(t, func() bool { return nativeProbe(p.BaseURL+"/api/ready") == 200 }, 30*time.Second, 100*time.Millisecond)
	}
	writeLoadJSON(t, filepath.Join(evidence, "native-fixture.json"), map[string]interface{}{"schema_prepared_by": "原 07 启动阶段，计时前已停止全部 Core 应用", "roles": []string{"user", "supplier", "order"}, "mtls": true, "mysql_max_open": 20, "mysql_max_idle": 20, "mysql_interpolate_params": true, "sync_writes": true, "peer_resolution": "测试夹具分配静态地址；不比较服务发现/注册自愈", "scope": "下单与同步事件负载，不是完整 07 功能替代"})
	return processes
}

func findReferenceFile(t *testing.T, root string, match func(string, []byte) bool) string {
	t.Helper()
	var result string
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		var b []byte
		if strings.HasSuffix(path, ".json") {
			b, e = os.ReadFile(path)
			if e != nil {
				return e
			}
		}
		if match(path, b) {
			if result != "" {
				return fmt.Errorf("匹配多个文件: %s", root)
			}
			result = path
		}
		return nil
	}))
	require.NotEmpty(t, result, "夹具缺少原生对照所需文件: %s", root)
	return result
}
