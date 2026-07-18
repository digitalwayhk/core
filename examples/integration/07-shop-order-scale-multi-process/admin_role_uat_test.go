// Package shoporderscalemultiprocess 验证 07 Docker 多进程下管理员角色的部署边界闭环。
// 本文件覆盖平台管理员准备基础资料、order 管理端口不对宿主机开放，以及双 order 副本注册唯一实例。
package shoporderscalemultiprocess

import (
	"net"
	"strconv"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

// TestDockerUATAdminRoleFlow 验证管理员角色在 Docker 多进程下可管理基础资料且不能从宿主机直连 order。
func TestDockerUATAdminRoleFlow(t *testing.T) {
	compose := startDockerOrderScaleStack(t)
	user := &integration.Suite{BaseURL: "http://127.0.0.1:18181"}
	supplier := &integration.Suite{BaseURL: "http://127.0.0.1:18182"}
	waitDockerUserReady(t, user)
	nodes := verifyDockerOrderReplicaDiscovery(t, compose)
	require.GreaterOrEqual(t, len(nodes), 2)

	adminToken := supplier.TokenFor(t, "platform-admin", 1)
	productID := addDockerSupplierProduct(t, supplier, adminToken)
	require.NotZero(t, productID)
	requireHostOrderPortClosed(t, 18183)
	requireHostOrderPortClosed(t, 18184)
}

// requireHostOrderPortClosed 验证 order 副本端口没有发布到宿主机。
func requireHostOrderPortClosed(t *testing.T, port int) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond)
	if conn != nil {
		_ = conn.Close()
	}
	require.Error(t, err, "shop-order 端口 %d 不应暴露到宿主机", port)
}
