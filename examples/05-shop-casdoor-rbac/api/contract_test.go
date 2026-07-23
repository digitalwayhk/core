package api_test

import (
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/dto"
	privateapi "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/private"
	publicapi "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/api/public"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/contract"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
)

func TestServiceAndRouteContracts(t *testing.T) {
	assert.Equal(t, "casdoorrbacshop", contract.ServiceName)

	for _, route := range []servertypes.IRouter{&publicapi.GetProducts{}, &publicapi.GetSuppliers{}, &publicapi.GetPaymentTypes{}} {
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

func TestOrderResponseContainsSupplierSnapshot(t *testing.T) {
	order := models.NewOrder()
	order.SetID(11)
	order.Status = int(models.OrderStatusCancelling)
	order.PaymentStatus = models.PaymentStatusRefunding
	order.PaymentID = 22
	order.SupplierID = 33
	order.SupplierCode = "supplier-a"
	order.SupplierName = "供应商 A"

	response := dto.NewOrderResponse(order)
	assert.Equal(t, "撤销处理中", response.StatusName)
	assert.Equal(t, "退款中", response.PaymentStatusName)
	assert.Equal(t, uint(33), response.SupplierID)
	assert.Equal(t, "supplier-a", response.SupplierCode)
	assert.Equal(t, "供应商 A", response.SupplierName)
}
