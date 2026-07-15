package performanceshop_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type shopSuite struct{ *integration.Suite }

type ProductDTO struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	Price        string `json:"price"`
	SupplierID   uint   `json:"supplierID"`
	SupplierCode string `json:"supplierCode"`
	SupplierName string `json:"supplierName"`
	Enabled      bool   `json:"enabled"`
}

type SupplierDTO struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

type PaymentTypeDTO struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description"`
}

type OrderDTO struct {
	Action            string `json:"action,omitempty"`
	ID                string `json:"id"`
	ProductID         uint   `json:"productID"`
	ProductCode       string `json:"productCode"`
	ProductName       string `json:"productName"`
	SupplierID        uint   `json:"supplierID"`
	SupplierCode      string `json:"supplierCode"`
	SupplierName      string `json:"supplierName"`
	UnitPrice         string `json:"unitPrice"`
	Quantity          int    `json:"quantity"`
	Amount            string `json:"amount"`
	UserID            string `json:"userID"`
	Status            int    `json:"status"`
	StatusName        string `json:"statusName"`
	PaymentStatus     int    `json:"paymentStatus"`
	PaymentStatusName string `json:"paymentStatusName"`
	PaymentID         string `json:"paymentID"`
}

func startShopSuite() (*shopSuite, error) {
	base, err := integration.StartProcess(integration.ProcessOptions{
		BuildPackage: "./examples/04-shop-performance/main", BinaryName: "shop-performance",
		TempPrefix: "core-shop-performance-", ServiceCount: 2, ServiceIndex: 1,
		Arguments:   []string{"-view", "0"},
		DisableRace: integration.IsBenchmarkRun(),
	})
	if err != nil {
		return nil, err
	}
	created := &shopSuite{Suite: base}
	if err := created.waitReady(); err != nil {
		created.Stop()
		return nil, err
	}
	for _, name := range []string{"server.json", "performanceshop.json"} {
		if _, err := os.Stat(filepath.Join(created.RootDir, "etc", name)); err != nil {
			created.Stop()
			return nil, fmt.Errorf("框架未自动生成配置 %s: %w", name, err)
		}
	}
	created.StopProcess()
	if err := enableLocalRouteCache(filepath.Join(created.RootDir, "etc", "performanceshop.json")); err != nil {
		created.Stop()
		return nil, err
	}
	if err := created.Restart(); err != nil {
		created.Stop()
		return nil, err
	}
	if err := created.waitReady(); err != nil {
		created.Stop()
		return nil, err
	}
	return created, nil
}

func enableLocalRouteCache(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("读取性能示例配置: %w", err)
	}
	var content map[string]interface{}
	if err := json.Unmarshal(data, &content); err != nil {
		return fmt.Errorf("解析性能示例配置: %w", err)
	}
	content["RouteCache"] = map[string]interface{}{
		"Mode": "local",
		"TTL":  int64(10 * time.Second),
		"L1": map[string]interface{}{
			"Limit": 4096,
		},
		"L2": map[string]interface{}{
			"Enable":           true,
			"Path":             filepath.Join(filepath.Dir(filepath.Dir(configPath)), "route-cache-l2"),
			"MaxBytes":         int64(64 << 20),
			"CorruptionPolicy": "fail",
		},
		"Redis": map[string]interface{}{
			"Prefix":        "digitalway:routecache",
			"OnUnavailable": "fail",
		},
	}
	content["MQ"] = map[string]interface{}{"Mode": "off"}
	if integration.IsBenchmarkRun() {
		logConfig, _ := content["Log"].(map[string]interface{})
		if logConfig == nil {
			logConfig = make(map[string]interface{})
		}
		logConfig["Level"] = "error"
		content["Log"] = logConfig
	}
	encoded, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return fmt.Errorf("编码性能示例配置: %w", err)
	}
	if err := os.WriteFile(configPath, encoded, 0o644); err != nil {
		return fmt.Errorf("写入性能示例配置: %w", err)
	}
	return nil
}

func (s *shopSuite) waitReady() error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(s.BaseURL + "/api/performanceshop/getproducts")
		if err == nil {
			var envelope integration.ResponseEnvelope
			_ = json.NewDecoder(response.Body).Decode(&envelope)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK && envelope.Success {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	data, _ := os.ReadFile(filepath.Join(s.RootDir, "service.log"))
	return fmt.Errorf("等待继承商城启动超时\n%s", data)
}

func (s *shopSuite) AddProduct(t testing.TB, token, name, price string) ProductDTO {
	t.Helper()
	suffix := time.Now().UnixNano()
	supplier := s.AddSupplier(t, token, fmt.Sprintf("supplier-%d", suffix), "供应商-"+name, true)
	return s.AddProductForSupplier(t, token, fmt.Sprintf("product-%d", suffix), name, price, uintID(t, supplier.ID), true)
}

func (s *shopSuite) AddProductForSupplier(t testing.TB, token, code, name, price string, supplierID uint, enabled bool) ProductDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/performanceshop/productmanage/add", token, map[string]interface{}{
		"code": code, "name": name, "price": price, "supplierID": supplierID, "enabled": true,
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var result ProductDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &result))
	require.False(t, result.Enabled, "基础资料新增时必须强制禁用")
	if enabled {
		s.SetBaseDataEnabled(t, token, "productmanage", result.ID, true)
		result.Enabled = true
	}
	return result
}

func (s *shopSuite) AddSupplier(t testing.TB, token, code, name string, enabled bool) SupplierDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/performanceshop/suppliermanage/add", token, map[string]interface{}{
		"code": code, "name": name, "enabled": true, "description": name + "说明",
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var result SupplierDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &result))
	require.False(t, result.Enabled, "基础资料新增时必须强制禁用")
	if enabled {
		s.SetBaseDataEnabled(t, token, "suppliermanage", result.ID, true)
		result.Enabled = true
	}
	return result
}

func (s *shopSuite) SetBaseDataEnabled(t testing.TB, token, manageName, id string, enabled bool) {
	t.Helper()
	command := "disablebasedata"
	if enabled {
		command = "enablebasedata"
	}
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/performanceshop/"+manageName+"/"+command, token, map[string]interface{}{"id": id})
	require.True(t, envelope.Success, envelope.ErrorMessage)
}

func (s *shopSuite) AddPaymentType(t testing.TB, token, code, name string, enabled bool) PaymentTypeDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/performanceshop/paymenttypemanage/add", token, map[string]interface{}{
		"code": code, "name": name, "enabled": enabled, "description": name + "说明",
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var result PaymentTypeDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &result))
	require.False(t, result.Enabled, "基础资料新增时必须强制禁用")
	if enabled {
		s.SetBaseDataEnabled(t, token, "paymenttypemanage", result.ID, true)
		result.Enabled = true
	}
	return result
}

func (s *shopSuite) AddOrder(t testing.TB, token string, productID uint, quantity int) OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/performanceshop/addorder", token, map[string]interface{}{"productID": productID, "quantity": quantity})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	return decodeOrder(t, envelope.Data)
}

func (s *shopSuite) CreatePayment(t testing.TB, token, orderID, paymentTypeID string) OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/performanceshop/createpayment", token, map[string]interface{}{
		"orderID": orderID, "paymentTypeID": paymentTypeID,
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	return decodeOrder(t, envelope.Data)
}

func (s *shopSuite) PaymentCommand(t testing.TB, token, command, paymentID string) OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/performanceshop/paymentrecordmanage/"+command, token, map[string]interface{}{"id": paymentID})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	return decodeOrder(t, envelope.Data)
}

func (s *shopSuite) GetOrders(t testing.TB, token string) []OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodGet, "/api/performanceshop/getorders", token, nil)
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var result []OrderDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &result))
	return result
}

func (s *shopSuite) ConnectAndSubscribe(t testing.TB, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(s.WebSocketURL, nil)
	require.NoError(t, err)
	s.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	require.Equal(t, "success", s.ReadWebSocket(t, connection, 3*time.Second).Event)
	s.WriteWebSocket(t, connection, "sub", "/api/performanceshop/getorders", map[string]interface{}{})
	require.Equal(t, "sub", s.ReadWebSocket(t, connection, 3*time.Second).Event)
	return connection
}

func (s *shopSuite) ReadOrderEvent(t testing.TB, connection *websocket.Conn) OrderDTO {
	t.Helper()
	message := s.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "/api/performanceshop/getorders", message.Channel)
	return decodeOrder(t, message.Data)
}

func decodeOrder(t testing.TB, data []byte) OrderDTO {
	t.Helper()
	var result OrderDTO
	require.NoError(t, json.Unmarshal(data, &result), string(data))
	return result
}

func uintID(t testing.TB, value string) uint {
	t.Helper()
	id, err := strconv.ParseUint(value, 10, 64)
	require.NoError(t, err)
	return uint(id)
}
