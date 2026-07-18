// Package shoporderscalemultiprocess 提供 07 Docker 多 order 副本的可选真实 UAT 辅助能力。
package shoporderscalemultiprocess

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	userdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/user"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// TestDockerComposeOrderScaleUAT 验证 Docker 下两个 order 副本共享 MySQL 并完成真实下单查询。
func TestDockerComposeOrderScaleUAT(t *testing.T) {
	compose := startDockerOrderScaleStack(t)
	user := &integration.Suite{BaseURL: "http://127.0.0.1:18181"}
	supplier := &integration.Suite{BaseURL: "http://127.0.0.1:18182"}
	waitDockerUserReady(t, user)
	verifyDockerOrderReplicaDiscovery(t, compose)

	adminToken := supplier.TokenFor(t, "platform-admin", 1)
	productID := addDockerSupplierProduct(t, supplier, adminToken)
	buyerToken := user.TokenFor(t, "720001", 0)
	requestID := "docker-uat-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	created := createDockerBuyerOrderWithRequest(t, user, buyerToken, productID, requestID)
	retried := createDockerBuyerOrderWithRequest(t, user, buyerToken, productID, requestID)
	require.Equal(t, created.OrderID, retried.OrderID)
	waitDockerOrderVisible(t, user, buyerToken, created.OrderID)
}

// startDockerOrderScaleStack 启动 07 Docker 双 order 副本测试栈。
func startDockerOrderScaleStack(t *testing.T) string {
	t.Helper()
	if os.Getenv("SHOP_RUN_DOCKER_UAT") != "1" {
		t.Skip("设置 SHOP_RUN_DOCKER_UAT=1 后运行真实 Docker 多副本 UAT")
	}
	compose := dockerComposeFile()
	runCompose(t, compose, "up", "-d", "--build", "mysql", "redis", "shop-user", "shop-supplier", "shop-order-a", "shop-order-b")
	t.Cleanup(func() { runCompose(t, compose, "down", "-v", "--remove-orphans") })
	return compose
}

// dockerComposeFile 返回 07 Docker Compose 文件路径。
func dockerComposeFile() string {
	return filepath.Join("..", "..", "07-shop-order-scale", "deploy", "docker-compose.yml")
}

// waitDockerOrderVisible 等待买家入口从共享 MySQL 权威库查询到指定订单。
func waitDockerOrderVisible(t *testing.T, user *integration.Suite, buyerToken string, orderID uint) {
	t.Helper()
	require.Eventually(t, func() bool {
		response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/getorders", buyerToken, map[string]interface{}{})
		if !response.Success {
			return false
		}
		var orders []*orderdto.Order
		require.NoError(t, json.Unmarshal(response.Data, &orders))
		for _, item := range orders {
			if item != nil && item.OrderID == orderID {
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

// runComposeOutput 执行 Docker Compose 命令并返回输出。
func runComposeOutput(compose string, args ...string) (string, error) {
	full := append([]string{"compose", "-f", compose}, args...)
	cmd := exec.Command("docker", full...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// verifyDockerOrderReplicaDiscovery 验证两个 order 副本注册了不同实例和 MachineID。
func verifyDockerOrderReplicaDiscovery(t *testing.T, compose string) []*cluster.NodeInfo {
	t.Helper()
	var nodes []*cluster.NodeInfo
	require.Eventually(t, func() bool {
		var err error
		nodes, err = dockerOrderNodes(compose)
		if err != nil || len(nodes) < 2 {
			return false
		}
		machineIDs := map[int64]bool{}
		instanceIDs := map[string]bool{}
		for _, node := range nodes {
			if node == nil || node.ServiceName != "shop-order" || node.MachineID < 0 || strings.TrimSpace(node.ServiceInstanceID) == "" {
				return false
			}
			machineIDs[node.MachineID] = true
			instanceIDs[node.ServiceInstanceID] = true
		}
		return len(machineIDs) >= 2 && len(instanceIDs) >= 2
	}, 15*time.Second, 300*time.Millisecond)
	return nodes
}

// dockerOrderNodes 从 Redis discovery 中读取当前 shop-order 注册节点。
func dockerOrderNodes(compose string) ([]*cluster.NodeInfo, error) {
	idsOutput, err := runComposeOutput(compose, "exec", "-T", "redis", "redis-cli", "--raw", "SMEMBERS", "core:discovery:07:services:shop-order")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(idsOutput)
	nodes := make([]*cluster.NodeInfo, 0, len(ids))
	for _, id := range ids {
		data, err := runComposeOutput(compose, "exec", "-T", "redis", "redis-cli", "--raw", "GET", "core:discovery:07:nodes:shop-order:"+id)
		if err != nil {
			return nil, err
		}
		node := &cluster.NodeInfo{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(data)), node); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
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
	return createDockerBuyerOrderWithRequest(t, user, buyerToken, productID, "docker-uat-"+strconv.FormatInt(time.Now().UnixNano(), 10))
}

// createDockerBuyerOrderWithRequest 使用指定 requestID 调用买家下单入口。
func createDockerBuyerOrderWithRequest(t *testing.T, user *integration.Suite, buyerToken string, productID uint, requestID string) orderdto.Order {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", buyerToken, map[string]interface{}{
		"productID": productID,
		"quantity":  2,
		"requestID": requestID,
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

// payDockerBuyerOrder 通过 user-service Private API 支付买家本人订单。
func payDockerBuyerOrder(t *testing.T, user *integration.Suite, buyerToken string, orderID uint) orderdto.Order {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/createpayment", buyerToken, map[string]interface{}{
		"orderID":       orderID,
		"paymentTypeID": 1,
		"paymentID":     "docker-payment-" + strconv.FormatInt(time.Now().UnixNano(), 10),
	})
	require.True(t, response.Success, response.ErrorMessage)
	var order orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &order))
	require.Equal(t, orderID, order.OrderID)
	return order
}

// cancelDockerBuyerOrder 通过 user-service Private API 撤销买家本人订单。
func cancelDockerBuyerOrder(t *testing.T, user *integration.Suite, buyerToken string, orderID uint) orderdto.Order {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/cancelorder", buyerToken, map[string]interface{}{
		"orderID": orderID,
	})
	require.True(t, response.Success, response.ErrorMessage)
	var order orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &order))
	require.Equal(t, orderID, order.OrderID)
	return order
}

// connectDockerBuyerOrdersWebSocket 登录并订阅 Docker user-service 买家订单 WebSocket。
func connectDockerBuyerOrdersWebSocket(t *testing.T, user *integration.Suite, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(user.WebSocketURL, nil)
	require.NoError(t, err)
	user.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	require.Equal(t, "success", user.ReadWebSocket(t, connection, 3*time.Second).Event)
	user.WriteWebSocket(t, connection, "sub", "/api/shop-user/getorders", map[string]interface{}{"page": 1, "size": 20})
	require.Equal(t, "sub", user.ReadWebSocket(t, connection, 3*time.Second).Event)
	return connection
}

// requireDockerOrderEvent 读取并校验买家订单 WebSocket 事件。
func requireDockerOrderEvent(t *testing.T, user *integration.Suite, connection *websocket.Conn, orderID uint, orderStatus, paymentStatus string) {
	t.Helper()
	message := user.ReadWebSocket(t, connection, 8*time.Second)
	var event orderdto.OrderChanged
	require.NoError(t, json.Unmarshal(message.Data, &event))
	require.Equal(t, orderID, event.OrderID)
	if strings.TrimSpace(orderStatus) != "" {
		require.Equal(t, orderStatus, event.OrderStatus)
	}
	if strings.TrimSpace(paymentStatus) != "" {
		require.Equal(t, paymentStatus, event.PaymentStatus)
	}
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
