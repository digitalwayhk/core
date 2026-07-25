// 本文件保存 06 三进程 UAT 中供应商角色的完整闭环。
// 供应商负责在供应商服务维护并上架自己的商品，
// 并验证只能在本服务看到属于自己的订单投影。
package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type threeProcessSupplierRole struct {
	token      string
	otherToken string
	product    supplierdto.Product
}

// TestThreeProcessUATSupplierRoleFlow 验证供应商角色可独立完成商品上架、订单投影查询和其他供应商隔离闭环。
func TestThreeProcessUATSupplierRoleFlow(t *testing.T) {
	scenario := startThreeProcessUAT(t)

	buyer := scenario.completeBuyerProfile()
	supplier := scenario.publishSupplierProduct()
	created := scenario.buyerCreatesOrder(buyer, supplier)

	scenario.assertSupplierCanSeeOwnOrder(supplier, created, buyer)
	scenario.assertOtherSupplierCannotSeeOrder(supplier, created)
}

// publishSupplierProduct 准备供应商角色 token，并完成商品创建与上架。
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

// addSupplierProduct 通过供应商 Manage API 创建商品并启用，供买家从用户服务 facade 下单。
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

// assertSupplierCanSeeOwnOrder 验证供应商服务通过订单事件投影看到自己的订单。
func (scenario *threeProcessUAT) assertSupplierCanSeeOwnOrder(supplier threeProcessSupplierRole, created orderdto.Order, buyer threeProcessBuyerRole) {
	t := scenarioTest(scenario)
	t.Helper()
	require.Eventually(t, func() bool {
		orders := scenario.searchSupplierOrders(supplier.token)
		found := findSupplierOrderByID(orders, created.ID)
		return found != nil && found.SupplierID == supplier.product.SupplierID && found.ProductID == supplier.product.ID && found.Address.Detail == buyer.address.Detail
	}, 5*time.Second, 25*time.Millisecond)
}

// assertOtherSupplierCannotSeeOrder 验证其他供应商不能查询到当前供应商的订单投影。
func (scenario *threeProcessUAT) assertOtherSupplierCannotSeeOrder(supplier threeProcessSupplierRole, created orderdto.Order) {
	t := scenarioTest(scenario)
	t.Helper()
	otherSupplierOrders := scenario.searchSupplierOrders(supplier.otherToken)
	require.Nil(t, findSupplierOrderByID(otherSupplierOrders, created.ID), "其他供应商不能查询到该订单")
}

// searchSupplierOrders 查询供应商服务本地订单投影，并转换为 SupplierOrder DTO 便于业务断言。
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
