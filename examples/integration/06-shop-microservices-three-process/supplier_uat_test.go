package shopmicroservices_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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

type threeProcessSupplierRole struct {
	token      string
	otherToken string
	product    supplierdto.Product
}

func (scenario *threeProcessUAT) publishSupplierProduct() threeProcessSupplierRole {
	t := scenarioTest(scenario)
	t.Helper()
	supplier := threeProcessSupplierRole{
		token:      scenario.supplier.TokenFor(t, "uat-supplier-"+scenario.suffix, 1),
		otherToken: scenario.supplier.TokenFor(t, "uat-other-supplier-"+scenario.suffix, 1),
	}
	supplier.product = scenario.addSupplierProduct(supplier.token, "uat-product-"+scenario.suffix)
	return supplier
}

func (scenario *threeProcessUAT) addSupplierProduct(token, code string) supplierdto.Product {
	t := scenarioTest(scenario)
	t.Helper()
	created := scenario.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/add", token, map[string]interface{}{"name": "三进程商品", "code": code, "price": "19.90"})
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
	enabled := scenario.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/setproductenabled", token, map[string]interface{}{"id": raw.ID, "enabled": true})
	require.True(t, enabled.Success, enabled.ErrorMessage)
	return supplierdto.Product{ID: uint(id), SupplierID: raw.SupplierID, Name: raw.Name, Code: raw.Code, Price: decimal.RequireFromString(raw.Price), Enabled: true}
}

func (scenario *threeProcessUAT) waitSupplierOrderProjectionReady() {
	t := scenarioTest(scenario)
	t.Helper()
	waitRedisConsumerGroup(t, scenario.redisPrefix+":event", contract.SubjectOrderCreated, contract.SupplierServiceName, scenario.user, scenario.supplier, scenario.order)
}

func (scenario *threeProcessUAT) assertSupplierCanSeeOwnOrder(supplier threeProcessSupplierRole, created orderdto.Order, buyer threeProcessBuyerRole) {
	t := scenarioTest(scenario)
	t.Helper()
	require.Eventually(t, func() bool {
		orders := scenario.searchSupplierOrders(supplier.token)
		found := findSupplierOrderByID(orders, created.ID)
		return found != nil && found.SupplierID == supplier.product.SupplierID && found.ProductID == supplier.product.ID && found.Address.Detail == buyer.address.Detail
	}, 5*time.Second, 25*time.Millisecond)
}

func (scenario *threeProcessUAT) assertOtherSupplierCannotSeeOrder(supplier threeProcessSupplierRole, created orderdto.Order) {
	t := scenarioTest(scenario)
	t.Helper()
	otherSupplierOrders := scenario.searchSupplierOrders(supplier.otherToken)
	require.Nil(t, findSupplierOrderByID(otherSupplierOrders, created.ID), "其他供应商不能查询到该订单")
}

func (scenario *threeProcessUAT) searchSupplierOrders(token string) []*orderdto.SupplierOrder {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/ordermanage/search", token, map[string]interface{}{"page": 1, "size": 100})
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

func waitRedisConsumerGroup(t require.TestingT, prefix, subject, group string, processes ...*integration.Suite) {
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
	require.FailNowf(t, "等待 Redis consumer group 超时", "key=%s group=%s", key, group)
}

func dumpThreeProcessLogs(processes ...*integration.Suite) {
	for index, process := range processes {
		if process != nil {
			fmt.Printf("process %d\n", index)
			process.PrintLog()
		}
	}
}
