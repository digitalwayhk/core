// Package shoporderscalemultiprocess 验证 07 Docker 多进程下供应商角色的跨服务闭环。
// 本文件覆盖供应商域管理商品、user-service facade 经内部受限 Public 查询到商品，以及 order 副本 discovery 正常。
package shoporderscalemultiprocess

import (
	"encoding/json"
	"net/http"
	"testing"

	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

// TestDockerUATSupplierRoleFlow 验证供应商角色在 Docker 多服务下发布商品并被用户入口查询。
func TestDockerUATSupplierRoleFlow(t *testing.T) {
	compose := startDockerOrderScaleStack(t)
	user := &integration.Suite{BaseURL: "http://127.0.0.1:18181"}
	supplier := &integration.Suite{BaseURL: "http://127.0.0.1:18182"}
	waitDockerUserReady(t, user)
	verifyDockerOrderReplicaDiscovery(t, compose)

	adminToken := supplier.TokenFor(t, "platform-admin", 1)
	productID := addDockerSupplierProduct(t, supplier, adminToken)

	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/getproducts", "", map[string]interface{}{})
	require.True(t, response.Success, response.ErrorMessage)
	var products []*supplierdto.Product
	require.NoError(t, json.Unmarshal(response.Data, &products))
	require.True(t, dockerProductExists(products, productID))
}

// dockerProductExists 判断 user facade 返回的商品列表中是否包含指定商品。
func dockerProductExists(products []*supplierdto.Product, productID uint) bool {
	for _, product := range products {
		if product != nil && product.ID == productID {
			return true
		}
	}
	return false
}
