package shopmicroservices_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/bootstrap"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type suiteSet struct{ base, user, supplier, order *integration.Suite }

var suites *suiteSet

func TestAllInOneTransportConfigIsLocalInsecureGRPC(t *testing.T) {
	cfg := bootstrap.LocalServiceConfig("shop-user", 28081, 2, 1)
	require.Equal(t, "local", cfg.Cluster.Provider)
	require.Equal(t, "grpc", cfg.Transport.Internal)
	require.Empty(t, cfg.Transport.Fallback)
	require.Equal(t, "insecure", cfg.Transport.GRPC.Security.Mode)
}

func TestMain(m *testing.M) {
	base, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/06-shop-microservices/main/all-in-one", BinaryName: "shop-microservices", TempPrefix: "core-shop-microservices-", ServiceCount: 4, ServiceIndex: 1, GRPCServiceCount: 4, Arguments: []string{"-view", "0"}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	supplier := *base
	supplier.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", base.BasePort+2)
	supplier.WebSocketURL = fmt.Sprintf("ws://127.0.0.1:%d/ws", base.BasePort+2)
	order := *base
	order.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", base.BasePort+3)
	order.WebSocketURL = fmt.Sprintf("ws://127.0.0.1:%d/ws", base.BasePort+3)
	suites = &suiteSet{base: base, user: base, supplier: &supplier, order: &order}
	if err := waitReady(); err != nil {
		base.PrintLog()
		base.Stop()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code := m.Run()
	if code != 0 {
		base.PrintLog()
	}
	base.Stop()
	os.Exit(code)
}

func waitReady() error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		user, eu := suites.user.DoJSON(http.MethodGet, "/api/shop-user/getproducts", "", nil)
		if eu == nil && user.Success {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("等待多服务商城启动超时")
}

func addProduct(t *testing.T, supplierID string) (supplierdto.Product, string) {
	token := suites.supplier.TokenFor(t, supplierID, 1)
	created := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/add", token, map[string]interface{}{"name": "集成商品", "code": fmt.Sprintf("product-%d", time.Now().UnixNano()), "price": "12.50"})
	require.True(t, created.Success, created.ErrorMessage)
	var raw struct {
		ID         string `json:"id"`
		SupplierID uint   `json:"supplierID"`
		Name, Code string
		Price      string `json:"price"`
		Enabled    bool   `json:"enabled"`
	}
	require.NoError(t, json.Unmarshal(created.Data, &raw))
	id, err := strconv.ParseUint(raw.ID, 10, 64)
	require.NoError(t, err)
	product := supplierdto.Product{ID: uint(id), SupplierID: raw.SupplierID, Name: raw.Name, Code: raw.Code, Price: decimal.RequireFromString(raw.Price), Enabled: raw.Enabled}
	updated := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/setproductenabled", token, map[string]interface{}{"id": raw.ID, "enabled": true})
	require.True(t, updated.Success, updated.ErrorMessage)
	product.Enabled = true
	return product, token
}
func addAddress(t *testing.T, userID string) (userdto.Address, string) {
	manageToken := suites.user.TokenFor(t, userID, 1)
	token := suites.user.TokenFor(t, userID, 0)
	response := suites.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/addressmanage/add", manageToken, map[string]interface{}{"recipient": "集成用户", "phone": "10086", "region": "测试区", "detail": "1 号"})
	require.True(t, response.Success, response.ErrorMessage)
	var raw struct {
		ID                               string `json:"id"`
		Recipient, Phone, Region, Detail string
	}
	require.NoError(t, json.Unmarshal(response.Data, &raw))
	id, err := strconv.ParseUint(raw.ID, 10, 64)
	require.NoError(t, err)
	address := userdto.Address{ID: uint(id), Recipient: raw.Recipient, Phone: raw.Phone, Region: raw.Region, Detail: raw.Detail}
	return address, token
}
func addOrder(t *testing.T, token string, productID, addressID uint) orderdto.Order {
	response := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", token, map[string]interface{}{"requestID": fmt.Sprintf("request-%d", time.Now().UnixNano()), "productID": productID, "quantity": 2, "addressID": addressID})
	require.True(t, response.Success, response.ErrorMessage)
	var order orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &order))
	return order
}
func addPaymentType(t *testing.T, code string) orderdto.PaymentType {
	t.Helper()
	admin := suites.order.TokenFor(t, "platform-admin", 1)
	response := suites.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/paymenttypemanage/add", admin, map[string]interface{}{"name": "集成支付", "code": code, "enabled": true})
	require.True(t, response.Success, response.ErrorMessage)
	var raw struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Code    string `json:"code"`
		Enabled bool   `json:"enabled"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &raw))
	id, err := strconv.ParseUint(raw.ID, 10, 64)
	require.NoError(t, err)
	enabled := suites.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/paymenttypemanage/setpaymenttypeenabled", admin, map[string]interface{}{"id": raw.ID, "enabled": true})
	require.True(t, enabled.Success, enabled.ErrorMessage)
	return orderdto.PaymentType{ID: uint(id), Name: raw.Name, Code: raw.Code, Enabled: true}
}
func connectAndSubscribe(t *testing.T, suite *integration.Suite, token, channel string) *websocket.Conn {
	connection, _, err := websocket.DefaultDialer.Dial(suite.WebSocketURL, nil)
	require.NoError(t, err)
	suite.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	require.Equal(t, "success", suite.ReadWebSocket(t, connection, 3*time.Second).Event)
	suite.WriteWebSocket(t, connection, "sub", channel, map[string]interface{}{})
	require.Equal(t, "sub", suite.ReadWebSocket(t, connection, 3*time.Second).Event)
	return connection
}
