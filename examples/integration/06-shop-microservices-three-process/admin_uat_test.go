package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type threeProcessAdminRole struct {
	token string
}

func (scenario *threeProcessUAT) admin() threeProcessAdminRole {
	t := scenarioTest(scenario)
	t.Helper()
	return threeProcessAdminRole{token: scenario.order.TokenFor(t, "platform-admin", 1)}
}

func (scenario *threeProcessUAT) configurePaymentType() orderdto.PaymentType {
	t := scenarioTest(scenario)
	t.Helper()
	admin := scenario.admin()
	response := scenario.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/paymenttypemanage/add", admin.token, map[string]interface{}{"name": "三进程支付", "code": "uat-pay-" + scenario.suffix, "enabled": true})
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
	enabled := scenario.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/paymenttypemanage/setpaymenttypeenabled", admin.token, map[string]interface{}{"id": raw.ID, "enabled": true})
	require.True(t, enabled.Success, enabled.ErrorMessage)
	return orderdto.PaymentType{ID: uint(id), Name: raw.Name, Code: raw.Code, Enabled: true}
}

func (scenario *threeProcessUAT) assertAdminCanSeeOrder(created orderdto.Order, supplier threeProcessSupplierRole, buyer threeProcessBuyerRole) {
	t := scenarioTest(scenario)
	t.Helper()
	admin := scenario.admin()
	require.Eventually(t, func() bool {
		orders := scenario.searchAdminOrders(admin.token)
		found := findOrderByID(orders, created.ID)
		return found != nil && found.UserID == created.UserID && found.SupplierID == supplier.product.SupplierID && found.ProductID == supplier.product.ID && found.Address.Detail == buyer.address.Detail
	}, 5*time.Second, 25*time.Millisecond)
}

func (scenario *threeProcessUAT) searchAdminOrders(token string) []*orderdto.Order {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/ordermanage/search", token, map[string]interface{}{"page": 1, "size": 100})
	require.True(t, response.Success, response.ErrorMessage)
	var table struct {
		Rows []struct {
			ID               string          `json:"id"`
			OrderRevision    uint64          `json:"orderRevision"`
			UserID           uint            `json:"userID"`
			SupplierID       uint            `json:"supplierID"`
			ProductID        uint            `json:"productID"`
			SupplierCode     string          `json:"supplierCode"`
			SupplierName     string          `json:"supplierName"`
			ProductCode      string          `json:"productCode"`
			ProductName      string          `json:"productName"`
			UnitPrice        decimal.Decimal `json:"unitPrice"`
			Quantity         int             `json:"quantity"`
			TotalAmount      decimal.Decimal `json:"totalAmount"`
			PaymentStatus    int             `json:"paymentStatus"`
			OrderStatus      int             `json:"orderStatus"`
			CurrentPaymentID string          `json:"currentPaymentID"`
			AddressID        uint            `json:"addressID"`
			Recipient        string          `json:"recipient"`
			Phone            string          `json:"phone"`
			Region           string          `json:"region"`
			AddressDetail    string          `json:"addressDetail"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &table))
	result := make([]*orderdto.Order, 0, len(table.Rows))
	for _, row := range table.Rows {
		id, err := strconv.ParseUint(row.ID, 10, 64)
		require.NoError(t, err)
		result = append(result, &orderdto.Order{ID: uint(id), OrderRevision: row.OrderRevision, UserID: row.UserID, SupplierID: row.SupplierID, ProductID: row.ProductID,
			SupplierCode: row.SupplierCode, SupplierName: row.SupplierName, ProductCode: row.ProductCode, ProductName: row.ProductName, UnitPrice: row.UnitPrice,
			Quantity: row.Quantity, TotalAmount: row.TotalAmount, PaymentStatus: row.PaymentStatus, OrderStatus: row.OrderStatus, CurrentPaymentID: row.CurrentPaymentID,
			Address: userdto.AddressSnapshot{AddressID: row.AddressID, Recipient: row.Recipient, Phone: row.Phone, Region: row.Region, Detail: row.AddressDetail}})
	}
	return result
}

func assertPaymentBelongsToOrder(t *testing.T, payment orderdto.PaymentRecord, order orderdto.Order) {
	t.Helper()
	require.Equal(t, order.ID, payment.OrderID)
}
