package api_test

import (
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/examples/02-shop-payment/api/dto"
	privateapi "github.com/digitalwayhk/core/examples/02-shop-payment/api/private"
	publicapi "github.com/digitalwayhk/core/examples/02-shop-payment/api/public"
	"github.com/digitalwayhk/core/examples/02-shop-payment/contract"
	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
)

func TestServiceAndRouteContracts(t *testing.T) {
	assert.Equal(t, "paymentshop", contract.ServiceName)

	for _, route := range []servertypes.IRouter{&publicapi.GetProducts{}, &publicapi.GetPaymentTypes{}} {
		info := route.RouterInfo()
		assert.Equal(t, http.MethodGet, info.GetMethod())
		assert.False(t, info.GetAuth())
	}

	for _, route := range []servertypes.IRouter{
		&privateapi.AddOrder{}, &privateapi.GetOrders{}, &privateapi.DeleteOrder{},
		&privateapi.CreatePayment{}, &privateapi.CancelOrder{},
	} {
		assert.True(t, route.RouterInfo().GetAuth())
	}
}

func TestOrderResponseContainsPaymentState(t *testing.T) {
	order := models.NewOrder()
	order.SetID(11)
	order.Status = models.OrderStatusCancelling
	order.PaymentStatus = models.PaymentStatusRefunding
	order.PaymentID = 22

	response := dto.NewOrderResponse(order)
	assert.Equal(t, 11, int(response.ID))
	assert.Equal(t, "撤销处理中", response.StatusName)
	assert.Equal(t, "退款中", response.PaymentStatusName)
	assert.Equal(t, 22, int(response.PaymentID))
}

func TestGetOrdersUsesSessionIdentityAndNoticeFilter(t *testing.T) {
	filter := &privateapi.GetOrders{}
	subscription := &privateapi.GetOrders{}
	subscription.SetUserID("user-a", "")

	assert.Implements(t, (*servertypes.IWebSocketUserIdentity)(nil), filter)
	assert.Implements(t, (*servertypes.IRouterHashKey)(nil), filter)
	assert.Implements(t, (*servertypes.IWebSocketRouterNotice)(nil), filter)

	accepted, _ := filter.NoticeFiltersRouter(&dto.OrderResponse{UserID: "user-a"}, subscription)
	assert.True(t, accepted)
	accepted, message := filter.NoticeFiltersRouter(&dto.OrderResponse{UserID: "user-b"}, subscription)
	assert.False(t, accepted)
	assert.Nil(t, message)
}
