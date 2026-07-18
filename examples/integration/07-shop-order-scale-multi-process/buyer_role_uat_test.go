// Package shoporderscalemultiprocess 验证 07 Docker 多进程下买家角色的跨服务闭环。
// 本文件覆盖买家经 user-service 下单、重复 requestID 幂等、order 双副本同步到共享 MySQL 后本人查询可见。
package shoporderscalemultiprocess

import (
	"strconv"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

// TestDockerUATBuyerRoleFlow 验证买家角色在 Docker 多 order 副本下的下单和订单查询闭环。
func TestDockerUATBuyerRoleFlow(t *testing.T) {
	compose := startDockerOrderScaleStack(t)
	user := &integration.Suite{BaseURL: "http://127.0.0.1:18181"}
	supplier := &integration.Suite{BaseURL: "http://127.0.0.1:18182"}
	waitDockerUserReady(t, user)
	verifyDockerOrderReplicaDiscovery(t, compose)

	adminToken := supplier.TokenFor(t, "platform-admin", 1)
	productID := addDockerSupplierProduct(t, supplier, adminToken)
	buyerToken := user.TokenFor(t, "docker-buyer-role", 0)
	requestID := "docker-buyer-role-" + strconv.FormatInt(time.Now().UnixNano(), 10)

	created := createDockerBuyerOrderWithRequest(t, user, buyerToken, productID, requestID)
	retried := createDockerBuyerOrderWithRequest(t, user, buyerToken, productID, requestID)
	require.Equal(t, created.OrderID, retried.OrderID)
	waitDockerOrderVisible(t, user, buyerToken, created.OrderID)
}
