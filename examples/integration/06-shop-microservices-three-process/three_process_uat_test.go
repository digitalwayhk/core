package shopmicroservices_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/bootstrap"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestThreeProcessUATThreeRolesOrderVisibility(t *testing.T) {
	pki := integration.NewGRPCTestPKI(t, "shop-user", "shop-supplier", "shop-order")
	redisPrefix := "core:test:06:three-process-uat:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	user, supplier, order := startShopProcesses(t, pki, redisPrefix)
	defer user.Stop()
	defer supplier.Stop()
	defer order.Stop()
	processes := []*integration.Suite{user, supplier, order}
	waitProcessReady(t, user, "/api/health", processes...)
	waitProcessReady(t, supplier, "/api/health", processes...)
	waitProcessReady(t, order, "/api/health", processes...)
	waitProcessReady(t, user, "/api/shop-user/getproducts", processes...)

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	buyerManageToken := user.TokenFor(t, "uat-buyer-"+suffix, 1)
	buyerToken := user.TokenFor(t, "uat-buyer-"+suffix, 0)
	otherBuyerToken := user.TokenFor(t, "uat-other-buyer-"+suffix, 0)
	updateUserName(t, user, buyerManageToken, "三进程买家")
	address := addThreeProcessAddress(t, user, buyerManageToken)

	supplierToken := supplier.TokenFor(t, "uat-supplier-"+suffix, 1)
	otherSupplierToken := supplier.TokenFor(t, "uat-other-supplier-"+suffix, 1)
	product := addThreeProcessProduct(t, supplier, supplierToken, "uat-product-"+suffix)
	paymentType := addThreeProcessPaymentType(t, order, "uat-pay-"+suffix)
	waitRedisConsumerGroup(t, redisPrefix+":event", contract.SubjectOrderCreated, contract.SupplierServiceName, processes...)

	created := createThreeProcessOrder(t, user, buyerToken, "uat-request-"+suffix, product.ID, address.ID)
	payment := createThreeProcessPayment(t, user, buyerToken, created.ID, paymentType.ID)
	require.Equal(t, created.ID, payment.OrderID)

	adminToken := order.TokenFor(t, "platform-admin", 1)
	require.Eventually(t, func() bool {
		orders := searchThreeProcessAdminOrders(t, order, adminToken)
		found := findOrderByID(orders, created.ID)
		return found != nil && found.UserID == created.UserID && found.SupplierID == product.SupplierID && found.ProductID == product.ID
	}, 5*time.Second, 25*time.Millisecond)

	require.Eventually(t, func() bool {
		orders := searchThreeProcessSupplierOrders(t, supplier, supplierToken)
		found := findSupplierOrderByID(orders, created.ID)
		return found != nil && found.SupplierID == product.SupplierID && found.ProductID == product.ID && found.Address.Detail == address.Detail
	}, 5*time.Second, 25*time.Millisecond)

	require.Eventually(t, func() bool {
		orders := getThreeProcessBuyerOrders(t, user, buyerToken)
		found := findOrderByID(orders, created.ID)
		return found != nil && found.UserID == created.UserID && found.Address.Detail == address.Detail
	}, 5*time.Second, 25*time.Millisecond)

	otherBuyerOrders := getThreeProcessBuyerOrders(t, user, otherBuyerToken)
	require.Nil(t, findOrderByID(otherBuyerOrders, created.ID), "其他普通用户不能查询到该订单")
	otherSupplierOrders := searchThreeProcessSupplierOrders(t, supplier, otherSupplierToken)
	require.Nil(t, findSupplierOrderByID(otherSupplierOrders, created.ID), "其他供应商不能查询到该订单")
}

func updateUserName(t *testing.T, user *integration.Suite, manageToken, name string) {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/usermanage/search", manageToken, map[string]interface{}{"page": 1, "size": 10})
	require.True(t, response.Success, response.ErrorMessage)
	var table struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &table))
	require.Len(t, table.Rows, 1)
	edited := user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/usermanage/edit", manageToken, map[string]interface{}{"id": table.Rows[0].ID, "name": name})
	require.True(t, edited.Success, edited.ErrorMessage)
}

func addThreeProcessAddress(t *testing.T, user *integration.Suite, manageToken string) userdto.Address {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/addressmanage/add", manageToken, map[string]interface{}{"recipient": "三进程买家", "phone": "10086", "region": "测试区", "detail": "三进程 1 号"})
	require.True(t, response.Success, response.ErrorMessage)
	var raw struct {
		ID                               string `json:"id"`
		Recipient, Phone, Region, Detail string
	}
	require.NoError(t, json.Unmarshal(response.Data, &raw))
	id, err := strconv.ParseUint(raw.ID, 10, 64)
	require.NoError(t, err)
	return userdto.Address{ID: uint(id), Recipient: raw.Recipient, Phone: raw.Phone, Region: raw.Region, Detail: raw.Detail}
}

func addThreeProcessProduct(t *testing.T, supplier *integration.Suite, token, code string) supplierdto.Product {
	t.Helper()
	created := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/add", token, map[string]interface{}{"name": "三进程商品", "code": code, "price": "19.90"})
	require.True(t, created.Success, created.ErrorMessage)
	var raw struct {
		ID         string `json:"id"`
		SupplierID uint   `json:"supplierID"`
		Name       string `json:"name"`
		Code       string `json:"code"`
		Price      string `json:"price"`
		Enabled    bool   `json:"enabled"`
	}
	require.NoError(t, json.Unmarshal(created.Data, &raw))
	id, err := strconv.ParseUint(raw.ID, 10, 64)
	require.NoError(t, err)
	enabled := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/setproductenabled", token, map[string]interface{}{"id": raw.ID, "enabled": true})
	require.True(t, enabled.Success, enabled.ErrorMessage)
	return supplierdto.Product{ID: uint(id), SupplierID: raw.SupplierID, Name: raw.Name, Code: raw.Code, Price: decimal.RequireFromString(raw.Price), Enabled: true}
}

func addThreeProcessPaymentType(t *testing.T, order *integration.Suite, code string) orderdto.PaymentType {
	t.Helper()
	adminToken := order.TokenFor(t, "platform-admin", 1)
	response := order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/paymenttypemanage/add", adminToken, map[string]interface{}{"name": "三进程支付", "code": code, "enabled": true})
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
	enabled := order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/paymenttypemanage/setpaymenttypeenabled", adminToken, map[string]interface{}{"id": raw.ID, "enabled": true})
	require.True(t, enabled.Success, enabled.ErrorMessage)
	return orderdto.PaymentType{ID: uint(id), Name: raw.Name, Code: raw.Code, Enabled: true}
}

func createThreeProcessOrder(t *testing.T, user *integration.Suite, token, requestID string, productID, addressID uint) orderdto.Order {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", token, map[string]interface{}{"requestID": requestID, "productID": productID, "quantity": 2, "addressID": addressID})
	require.True(t, response.Success, response.ErrorMessage)
	var result orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &result))
	return result
}

func createThreeProcessPayment(t *testing.T, user *integration.Suite, token string, orderID, paymentTypeID uint) orderdto.PaymentRecord {
	t.Helper()
	response := user.RequestJSON(t, http.MethodPost, "/api/shop-user/createpayment", token, map[string]interface{}{"orderID": orderID, "paymentTypeID": paymentTypeID})
	require.True(t, response.Success, response.ErrorMessage)
	var result orderdto.PaymentRecord
	require.NoError(t, json.Unmarshal(response.Data, &result))
	return result
}

func getThreeProcessBuyerOrders(t *testing.T, user *integration.Suite, token string) []*orderdto.Order {
	t.Helper()
	response := user.RequestJSON(t, http.MethodGet, "/api/shop-user/getorders", token, nil)
	require.True(t, response.Success, response.ErrorMessage)
	var orders []*orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &orders))
	return orders
}

func searchThreeProcessAdminOrders(t *testing.T, order *integration.Suite, token string) []*orderdto.Order {
	t.Helper()
	response := order.RequestJSON(t, http.MethodPost, "/api/manage/shop-order/ordermanage/search", token, map[string]interface{}{"page": 1, "size": 100})
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

func searchThreeProcessSupplierOrders(t *testing.T, supplier *integration.Suite, token string) []*orderdto.SupplierOrder {
	t.Helper()
	response := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/ordermanage/search", token, map[string]interface{}{"page": 1, "size": 100})
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

func findOrderByID(orders []*orderdto.Order, id uint) *orderdto.Order {
	for _, order := range orders {
		if order != nil && order.ID == id {
			return order
		}
	}
	return nil
}

func findSupplierOrderByID(orders []*orderdto.SupplierOrder, id uint) *orderdto.SupplierOrder {
	for _, order := range orders {
		if order != nil && order.OrderID == id {
			return order
		}
	}
	return nil
}

func waitRedisConsumerGroup(t *testing.T, prefix, subject, group string, processes ...*integration.Suite) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: bootstrap.RedisAddress()})
	defer client.Close()
	key := prefix + ":" + subject
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		groups, err := client.XInfoGroups(context.Background(), key).Result()
		if err == nil {
			for _, item := range groups {
				if item.Name == group {
					return
				}
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	dumpThreeProcessLogs(processes...)
	t.Fatalf("等待 Redis consumer group 超时: key=%s group=%s", key, group)
}

func dumpThreeProcessLogs(processes ...*integration.Suite) {
	for index, process := range processes {
		if process != nil {
			fmt.Printf("process %d\n", index)
			process.PrintLog()
		}
	}
}
