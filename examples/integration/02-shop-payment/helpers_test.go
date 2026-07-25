package paymentshop_test

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
	ID    string `json:"id"`
	Name  string `json:"name"`
	Price string `json:"price"`
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
	ProductName       string `json:"productName"`
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
		BuildPackage: "./examples/02-shop-payment/main", BinaryName: "shop-payment",
		TempPrefix: "core-shop-payment-", ServiceCount: 2, ServiceIndex: 1,
		Arguments: []string{"-view", "0"},
	})
	if err != nil {
		return nil, err
	}
	created := &shopSuite{Suite: base}
	if err := created.waitReady(); err != nil {
		created.Stop()
		return nil, err
	}
	for _, name := range []string{"server.json", "paymentshop.json"} {
		if _, err := os.Stat(filepath.Join(created.RootDir, "etc", name)); err != nil {
			created.Stop()
			return nil, fmt.Errorf("框架未自动生成配置 %s: %w", name, err)
		}
	}
	return created, nil
}

func (s *shopSuite) waitReady() error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(s.BaseURL + "/api/paymentshop/getproducts")
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
	return fmt.Errorf("等待支付商城启动超时\n%s", data)
}

func (s *shopSuite) AddProduct(t *testing.T, token, name, price string) ProductDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/paymentshop/productmanage/add", token, map[string]interface{}{"name": name, "price": price})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var result ProductDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &result))
	return result
}

func (s *shopSuite) AddPaymentType(t *testing.T, token, code, name string, enabled bool) PaymentTypeDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/paymentshop/paymenttypemanage/add", token, map[string]interface{}{
		"code": code, "name": name, "enabled": enabled, "description": name + "说明",
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var result PaymentTypeDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &result))
	return result
}

func (s *shopSuite) AddOrder(t *testing.T, token string, productID uint, quantity int) OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/paymentshop/addorder", token, map[string]interface{}{"productID": productID, "quantity": quantity})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	return decodeOrder(t, envelope.Data)
}

func (s *shopSuite) CreatePayment(t *testing.T, token, orderID, paymentTypeID string) OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/paymentshop/createpayment", token, map[string]interface{}{
		"orderID": orderID, "paymentTypeID": paymentTypeID,
	})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	return decodeOrder(t, envelope.Data)
}

func (s *shopSuite) PaymentCommand(t *testing.T, token, command, paymentID string) OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodPost, "/api/manage/paymentshop/paymentrecordmanage/"+command, token, map[string]interface{}{"id": paymentID})
	require.True(t, envelope.Success, envelope.ErrorMessage)
	return decodeOrder(t, envelope.Data)
}

func (s *shopSuite) GetOrders(t *testing.T, token string) []OrderDTO {
	t.Helper()
	envelope := s.RequestJSON(t, http.MethodGet, "/api/paymentshop/getorders", token, nil)
	require.True(t, envelope.Success, envelope.ErrorMessage)
	var result []OrderDTO
	require.NoError(t, json.Unmarshal(envelope.Data, &result))
	return result
}

func (s *shopSuite) ConnectAndSubscribe(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.DefaultDialer.Dial(s.WebSocketURL, nil)
	require.NoError(t, err)
	s.WriteWebSocket(t, connection, "sub", "logon", map[string]string{"token": token})
	require.Equal(t, "success", s.ReadWebSocket(t, connection, 3*time.Second).Event)
	s.WriteWebSocket(t, connection, "sub", "/api/paymentshop/getorders", map[string]interface{}{})
	require.Equal(t, "sub", s.ReadWebSocket(t, connection, 3*time.Second).Event)
	return connection
}

func (s *shopSuite) ReadOrderEvent(t *testing.T, connection *websocket.Conn) OrderDTO {
	t.Helper()
	message := s.ReadWebSocket(t, connection, 3*time.Second)
	require.Equal(t, "/api/paymentshop/getorders", message.Channel)
	return decodeOrder(t, message.Data)
}

func decodeOrder(t *testing.T, data []byte) OrderDTO {
	t.Helper()
	var result OrderDTO
	require.NoError(t, json.Unmarshal(data, &result), string(data))
	return result
}

func uintID(t *testing.T, value string) uint {
	t.Helper()
	id, err := strconv.ParseUint(value, 10, 64)
	require.NoError(t, err)
	return uint(id)
}
