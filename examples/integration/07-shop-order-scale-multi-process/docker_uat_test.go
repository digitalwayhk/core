// Package shoporderscalemultiprocess 提供 07 Docker 多 order 副本的可选真实 UAT。
package shoporderscalemultiprocess

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	userdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/user"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

// TestDockerComposeOrderScaleUAT 验证 Docker 下两个 order 副本共享 MySQL 并完成真实下单查询。
func TestDockerComposeOrderScaleUAT(t *testing.T) {
	if os.Getenv("SHOP_RUN_DOCKER_UAT") != "1" {
		t.Skip("设置 SHOP_RUN_DOCKER_UAT=1 后运行真实 Docker 多副本 UAT")
	}
	compose := filepath.Join("..", "..", "07-shop-order-scale", "deploy", "docker-compose.yml")
	runCompose(t, compose, "up", "-d", "--build", "mysql", "redis", "shop-user", "shop-supplier", "shop-order-a", "shop-order-b")
	t.Cleanup(func() { runCompose(t, compose, "down", "-v", "--remove-orphans") })

	user := &integration.Suite{BaseURL: "http://127.0.0.1:18181"}
	supplier := &integration.Suite{BaseURL: "http://127.0.0.1:18182"}
	waitDockerUserReady(t, user)

	adminToken := supplier.TokenFor(t, "platform-admin", 1)
	productID := addDockerSupplierProduct(t, supplier, adminToken)
	buyerToken := user.TokenFor(t, "720001", 0)
	created := createDockerBuyerOrder(t, user, buyerToken, productID)
	require.Eventually(t, func() bool {
		response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/getorders", buyerToken, map[string]interface{}{})
		if !response.Success {
			return false
		}
		var orders []*orderdto.Order
		require.NoError(t, json.Unmarshal(response.Data, &orders))
		for _, item := range orders {
			if item != nil && item.OrderID == created.OrderID {
				return true
			}
		}
		return false
	}, 10*time.Second, 300*time.Millisecond)
}

func runCompose(t *testing.T, compose string, args ...string) {
	t.Helper()
	full := append([]string{"compose", "-f", compose}, args...)
	cmd := exec.Command("docker", full...)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "docker %v\n%s", full, string(output))
}

func waitDockerUserReady(t *testing.T, suite *integration.Suite) {
	t.Helper()
	require.Eventually(t, func() bool {
		response, err := suite.DoJSON(http.MethodPost, "/api/shop-user/getproducts", "", map[string]interface{}{})
		return err == nil && response.Success
	}, 30*time.Second, 500*time.Millisecond)
}

func addDockerSupplierProduct(t *testing.T, supplier *integration.Suite, adminToken string) uint {
	t.Helper()
	unique := strconv.FormatInt(time.Now().UnixNano(), 10)
	supplierResponse := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/suppliermanage/add", adminToken, map[string]interface{}{
		"userID":      920001,
		"code":        "docker-supplier-" + unique,
		"name":        "07 Docker供应商",
		"description": "Docker UAT",
		"enabled":     true,
	})
	require.True(t, supplierResponse.Success, supplierResponse.ErrorMessage)
	supplierID := parseDockerManageID(t, supplierResponse.Data)

	productResponse := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/add", adminToken, map[string]interface{}{
		"supplierID": supplierID,
		"code":       "docker-product-" + unique,
		"name":       "07 Docker商品",
		"price":      "21.00",
		"enabled":    true,
	})
	require.True(t, productResponse.Success, productResponse.ErrorMessage)
	return parseDockerManageID(t, productResponse.Data)
}

func createDockerBuyerOrder(t *testing.T, user *integration.Suite, buyerToken string, productID uint) orderdto.Order {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", buyerToken, map[string]interface{}{
		"productID": productID,
		"quantity":  2,
		"requestID": "docker-uat-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		"address": userdto.AddressSnapshot{
			AddressID:    1,
			ReceiverName: "Docker买家",
			Phone:        "13800000000",
			Province:     "广东",
			City:         "深圳",
			District:     "南山",
			Detail:       "科技园",
		},
	})
	require.True(t, response.Success, response.ErrorMessage)
	var order orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &order))
	require.NotZero(t, order.OrderID)
	return order
}

func parseDockerManageID(t *testing.T, data json.RawMessage) uint {
	t.Helper()
	var object map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &object))
	if id, ok := object["ID"].(float64); ok {
		return uint(id)
	}
	if id, ok := object["id"].(float64); ok {
		return uint(id)
	}
	t.Fatalf("响应缺少 ID: %s", string(data))
	return 0
}
