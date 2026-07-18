package shopmicroservices_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/stretchr/testify/require"
)

type threeProcessBuyerRole struct {
	manageToken string
	token       string
	otherToken  string
	address     userdto.Address
}

func (scenario *threeProcessUAT) completeBuyerProfile() threeProcessBuyerRole {
	buyer := threeProcessBuyerRole{
		manageToken: scenario.user.TokenFor(scenarioTest(scenario), "uat-buyer-"+scenario.suffix, 1),
		token:       scenario.user.TokenFor(scenarioTest(scenario), "uat-buyer-"+scenario.suffix, 0),
		otherToken:  scenario.user.TokenFor(scenarioTest(scenario), "uat-other-buyer-"+scenario.suffix, 0),
	}
	scenario.updateBuyerName(buyer.manageToken, "三进程买家")
	buyer.address = scenario.addBuyerAddress(buyer.manageToken)
	return buyer
}

func (scenario *threeProcessUAT) updateBuyerName(manageToken, name string) {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/usermanage/search", manageToken, map[string]interface{}{"page": 1, "size": 10})
	require.True(t, response.Success, response.ErrorMessage)
	var table struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &table))
	require.Len(t, table.Rows, 1)
	edited := scenario.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/usermanage/edit", manageToken, map[string]interface{}{"id": table.Rows[0].ID, "name": name})
	require.True(t, edited.Success, edited.ErrorMessage)
}

func (scenario *threeProcessUAT) addBuyerAddress(manageToken string) userdto.Address {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/addressmanage/add", manageToken, map[string]interface{}{"recipient": "三进程买家", "phone": "10086", "region": "测试区", "detail": "三进程 1 号"})
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

func (scenario *threeProcessUAT) buyerCreatesOrder(buyer threeProcessBuyerRole, supplier threeProcessSupplierRole) orderdto.Order {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", buyer.token, map[string]interface{}{"requestID": "uat-request-" + scenario.suffix, "productID": supplier.product.ID, "quantity": 2, "addressID": buyer.address.ID})
	require.True(t, response.Success, response.ErrorMessage)
	var result orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &result))
	return result
}

func (scenario *threeProcessUAT) buyerCreatesPayment(buyer threeProcessBuyerRole, order orderdto.Order, paymentType orderdto.PaymentType) orderdto.PaymentRecord {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodPost, "/api/shop-user/createpayment", buyer.token, map[string]interface{}{"orderID": order.ID, "paymentTypeID": paymentType.ID})
	require.True(t, response.Success, response.ErrorMessage)
	var result orderdto.PaymentRecord
	require.NoError(t, json.Unmarshal(response.Data, &result))
	return result
}

func (scenario *threeProcessUAT) assertBuyerCanSeeOwnOrder(buyer threeProcessBuyerRole, created orderdto.Order) {
	t := scenarioTest(scenario)
	t.Helper()
	require.Eventually(t, func() bool {
		orders := scenario.getBuyerOrders(buyer.token)
		found := findOrderByID(orders, created.ID)
		return found != nil && found.UserID == created.UserID && found.Address.Detail == buyer.address.Detail
	}, 5*time.Second, 25*time.Millisecond)
}

func (scenario *threeProcessUAT) assertOtherBuyerCannotSeeOrder(buyer threeProcessBuyerRole, created orderdto.Order) {
	t := scenarioTest(scenario)
	t.Helper()
	otherBuyerOrders := scenario.getBuyerOrders(buyer.otherToken)
	require.Nil(t, findOrderByID(otherBuyerOrders, created.ID), "其他普通用户不能查询到该订单")
}

func (scenario *threeProcessUAT) getBuyerOrders(token string) []*orderdto.Order {
	t := scenarioTest(scenario)
	t.Helper()
	response := scenario.user.RequestJSON(t, http.MethodGet, "/api/shop-user/getorders", token, nil)
	require.True(t, response.Success, response.ErrorMessage)
	var orders []*orderdto.Order
	require.NoError(t, json.Unmarshal(response.Data, &orders))
	return orders
}
