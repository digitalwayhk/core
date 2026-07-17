package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestUATBuyerOrderLifecycle(t *testing.T) {
	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	product, supplierToken := addProduct(t, "uat-supplier-"+suffix)
	address, buyerToken := addAddress(t, "uat-buyer-"+suffix)
	request := map[string]interface{}{"requestID": "client-request-" + suffix, "productID": product.ID, "quantity": 2, "addressID": address.ID}
	first := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", buyerToken, request)
	require.True(t, first.Success, first.ErrorMessage)
	second := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", buyerToken, request)
	require.True(t, second.Success, second.ErrorMessage)
	var created, repeated orderdto.Order
	require.NoError(t, json.Unmarshal(first.Data, &created))
	require.NoError(t, json.Unmarshal(second.Data, &repeated))
	require.Equal(t, created.ID, repeated.ID)
	require.True(t, created.TotalAmount.Equal(decimal.NewFromInt(25)))

	require.Eventually(t, func() bool {
		orders := getSupplierOrders(t, supplierToken)
		return len(orders) == 1 && orders[0].OrderID == created.ID && orders[0].Address.Detail == address.Detail
	}, 5*time.Second, 25*time.Millisecond)

	paymentType := addPaymentType(t, "uat-pay-"+suffix)
	paying := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/createpayment", buyerToken, map[string]interface{}{"orderID": created.ID, "paymentTypeID": paymentType.ID})
	require.True(t, paying.Success, paying.ErrorMessage)
	var payment orderdto.PaymentRecord
	require.NoError(t, json.Unmarshal(paying.Data, &payment))
	admin := suites.order.TokenFor(t, "platform-admin", 1)
	confirmed := suites.order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/paymentrecordmanage/confirmpayment", admin, map[string]interface{}{"paymentID": payment.PaymentID})
	require.True(t, confirmed.Success, confirmed.ErrorMessage)

	require.Eventually(t, func() bool {
		orders := getBuyerOrders(t, buyerToken)
		return len(orders) == 1 && orders[0].PaymentStatus == models.PaymentStatusPaid && orders[0].OrderRevision >= 3
	}, 5*time.Second, 25*time.Millisecond)

	cancelled := suites.user.RequestJSON(t, http.MethodPost, "/api/shop-user/cancelorder", buyerToken, map[string]interface{}{"orderID": created.ID})
	require.True(t, cancelled.Success, cancelled.ErrorMessage)
	require.Eventually(t, func() bool {
		orders := getBuyerOrders(t, buyerToken)
		return len(orders) == 1 && orders[0].ID == created.ID && orders[0].OrderStatus == models.OrderStatusCancelling
	}, 5*time.Second, 25*time.Millisecond)
}

func getBuyerOrders(t *testing.T, token string) []*orderdto.Order {
	t.Helper()
	response := suites.user.RequestJSON(t, http.MethodGet, "/api/shop-user/getorders", token, nil)
	require.True(t, response.Success, response.ErrorMessage)
	var orders []*orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &orders))
	return orders
}

func getSupplierOrders(t *testing.T, token string) []*orderdto.SupplierOrder {
	t.Helper()
	response := suites.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/ordermanage/search", token, map[string]interface{}{"page": 1, "size": 100})
	require.True(t, response.Success, response.ErrorMessage)
	var table struct {
		Rows []struct {
			OrderID       uint            `json:"orderID"`
			OrderRevision uint64          `json:"orderRevision"`
			SupplierID    uint            `json:"supplierID"`
			ProductID     uint            `json:"productID"`
			SupplierCode  string          `json:"supplierCode"`
			SupplierName  string          `json:"supplierName"`
			ProductCode   string          `json:"productCode"`
			ProductName   string          `json:"productName"`
			UnitPrice     decimal.Decimal `json:"unitPrice"`
			Quantity      int             `json:"quantity"`
			TotalAmount   decimal.Decimal `json:"totalAmount"`
			PaymentStatus int             `json:"paymentStatus"`
			OrderStatus   int             `json:"orderStatus"`
			AddressID     uint            `json:"addressID"`
			Recipient     string          `json:"recipient"`
			Phone         string          `json:"phone"`
			Region        string          `json:"region"`
			AddressDetail string          `json:"addressDetail"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &table))
	result := make([]*orderdto.SupplierOrder, 0, len(table.Rows))
	for _, row := range table.Rows {
		result = append(result, &orderdto.SupplierOrder{OrderID: row.OrderID, OrderRevision: row.OrderRevision, SupplierID: row.SupplierID, ProductID: row.ProductID,
			SupplierCode: row.SupplierCode, SupplierName: row.SupplierName, ProductCode: row.ProductCode, ProductName: row.ProductName, UnitPrice: row.UnitPrice,
			Quantity: row.Quantity, TotalAmount: row.TotalAmount, PaymentStatus: row.PaymentStatus, OrderStatus: row.OrderStatus,
			Address: userdto.AddressSnapshot{AddressID: row.AddressID, Recipient: row.Recipient, Phone: row.Phone, Region: row.Region, Detail: row.AddressDetail}})
	}
	return result
}
