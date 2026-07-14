package simpleshop_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// shopSuite 在通用进程测试能力上增加最简商城专属的 DTO、路由和订阅辅助方法。
type shopSuite struct {
	*integration.Suite
}

// ProductDTO 是商城集成测试关注的商品公开字段。
type ProductDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Price string `json:"price"`
}

// OrderDTO 是商城集成测试关注的订单公开字段。
type OrderDTO struct {
	Action      string `json:"action,omitempty"`
	ID          string `json:"id"`
	ProductID   uint   `json:"productID"`
	ProductName string `json:"productName"`
	UnitPrice   string `json:"unitPrice"`
	Quantity    int    `json:"quantity"`
	UserID      string `json:"userID"`
	CreatedAt   string `json:"createdAt"`
}

// startShopSuite 启动真实商城进程，并等待框架自动生成配置和业务路由可用。
func startShopSuite() (*shopSuite, error) {
	base, err := integration.StartProcess(integration.ProcessOptions{
		BuildPackage: "./examples/01-simple-shop/main",
		BinaryName:   "simple-shop",
		TempPrefix:   "core-simple-shop-",
		ServiceCount: 2,
		ServiceIndex: 1,
		Arguments:    []string{"-view", "0"},
	})
	if err != nil {
		return nil, err
	}
	created := &shopSuite{Suite: base}
	if err := created.waitReady(); err != nil {
		created.Stop()
		return nil, err
	}
	for _, name := range []string{"server.json", "shop.json"} {
		if _, err := os.Stat(filepath.Join(created.RootDir, "etc", name)); err != nil {
			created.Stop()
			return nil, fmt.Errorf("框架未自动生成配置 %s: %w", name, err)
		}
	}
	return created, nil
}

// waitReady 轮询认证、商品和订单路由，确认 HTTP 与延迟数据表均已就绪。
func (s *shopSuite) waitReady() error {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		tokenResponse, err := http.Get(s.BaseURL + "/api/servermanage/testtoken?userid=health")
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var tokenEnvelope integration.ResponseEnvelope
		_ = json.NewDecoder(tokenResponse.Body).Decode(&tokenEnvelope)
		_ = tokenResponse.Body.Close()
		var token string
		_ = json.Unmarshal(tokenEnvelope.Data, &token)
		if tokenResponse.StatusCode != http.StatusOK || !tokenEnvelope.Success || token == "" {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		productsResponse, err := http.Get(s.BaseURL + "/api/shop/getproducts")
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var productsEnvelope integration.ResponseEnvelope
		_ = json.NewDecoder(productsResponse.Body).Decode(&productsEnvelope)
		_ = productsResponse.Body.Close()
		if productsResponse.StatusCode != http.StatusOK || !productsEnvelope.Success {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		ordersRequest, err := http.NewRequest(http.MethodGet, s.BaseURL+"/api/shop/getorders", nil)
		if err != nil {
			return err
		}
		ordersRequest.Header.Set("Authorization", "Bearer "+token)
		ordersResponse, err := http.DefaultClient.Do(ordersRequest)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var ordersEnvelope integration.ResponseEnvelope
		_ = json.NewDecoder(ordersResponse.Body).Decode(&ordersEnvelope)
		_ = ordersResponse.Body.Close()
		if ordersResponse.StatusCode == http.StatusOK && ordersEnvelope.Success {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(filepath.Join(s.RootDir, "service.log"))
	return fmt.Errorf("等待商城服务启动超时\n%s", data)
}

// AddProduct 通过真实 Manage Add 路由创建商品并返回公开字段。
func (s *shopSuite) AddProduct(t *testing.T, adminToken, name, price string) ProductDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/shop/productmanage/add", adminToken, map[string]interface{}{
		"name": name, "price": price,
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var product ProductDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &product))
	require.NotEmpty(t, product.ID)
	return product
}

// GetProducts 通过公开路由按查询条件获取商品响应模型。
func (s *shopSuite) GetProducts(t *testing.T, query string) []ProductDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodGet, "/api/shop/getproducts"+query, "", nil)
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var products []ProductDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &products))
	return products
}

// AddOrder 为其他 API 测试准备归属当前用户的订单数据。
func (s *shopSuite) AddOrder(t *testing.T, token, productID string, quantity int) OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/shop/addorder", token, map[string]interface{}{
		"productID": UintID(t, productID), "quantity": quantity,
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var order OrderDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &order))
	require.NotEmpty(t, order.ID)
	return order
}

// GetOrders 获取当前用户的订单响应模型。
func (s *shopSuite) GetOrders(t *testing.T, token string) []OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodGet, "/api/shop/getorders", token, nil)
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var orders []OrderDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &orders))
	return orders
}

// UintID 将框架以字符串编码的商城模型 ID 转换为 uint。
func UintID(t *testing.T, id string) uint {
	t.Helper()
	value, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	require.NoError(t, err)
	return uint(value)
}

// ConnectAndSubscribe 登录 WebSocket 并订阅当前用户的商城订单。
func (s *shopSuite) ConnectAndSubscribe(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(s.WebSocketURL, nil)
	require.NoError(t, err)
	s.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	logon := s.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "success", logon.Event, string(logon.Data))
	require.Equal(t, "logon", logon.Channel)

	s.WriteWebSocket(t, connection, "sub", "/api/shop/getorders", map[string]interface{}{})
	subscribed := s.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "sub", subscribed.Event, string(subscribed.Data))
	require.Equal(t, "/api/shop/getorders", subscribed.Channel)
	return connection
}

// ReadOrderEvent 读取并解析当前用户的订单变更 DTO。
func (s *shopSuite) ReadOrderEvent(t *testing.T, connection *websocket.Conn) OrderDTO {
	t.Helper()
	message := s.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "/api/shop/getorders", message.Channel)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(message.Data, &fields), string(message.Data))
	require.Contains(t, fields, "action")
	require.NotContains(t, fields, "order")
	var order OrderDTO
	require.NoError(t, json.Unmarshal(message.Data, &order), string(message.Data))
	return order
}

// AssertNoOrderEvent 验证另一用户在短窗口内没有收到商城订单通知。
func AssertNoOrderEvent(t *testing.T, messages <-chan integration.WebSocketMessage) {
	t.Helper()
	select {
	case message := <-messages:
		t.Fatalf("其他用户不应收到订单事件: %+v", message)
	case <-time.After(250 * time.Millisecond):
	}
}

// ProductNames 提取商品名称，简化列表断言。
func ProductNames(products []ProductDTO) []string {
	names := make([]string, 0, len(products))
	for _, product := range products {
		names = append(names, product.Name)
	}
	return names
}

// OrderIDs 提取订单 ID，简化所有权与删除结果断言。
func OrderIDs(orders []OrderDTO) []string {
	ids := make([]string, 0, len(orders))
	for _, order := range orders {
		ids = append(ids, order.ID)
	}
	return ids
}
