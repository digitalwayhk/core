package shopmicroservices_test

import (
	"strconv"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
)

type threeProcessUAT struct {
	t        *testing.T
	user     *integration.Suite
	supplier *integration.Suite
	order    *integration.Suite
	suffix   string
}

func TestThreeProcessUATThreeRolesOrderVisibility(t *testing.T) {
	scenario := startThreeProcessUAT(t)

	buyer := scenario.completeBuyerProfile()
	supplier := scenario.publishSupplierProduct()
	paymentType := scenario.configurePaymentType()

	created := scenario.buyerCreatesOrder(buyer, supplier)
	payment := scenario.buyerCreatesPayment(buyer, created, paymentType)
	assertPaymentBelongsToOrder(t, payment, created)

	scenario.assertAdminCanSeeOrder(created, supplier, buyer)
	scenario.assertSupplierCanSeeOwnOrder(supplier, created, buyer)
	scenario.assertBuyerCanSeeOwnOrder(buyer, created)
	scenario.assertOtherBuyerCannotSeeOrder(buyer, created)
	scenario.assertOtherSupplierCannotSeeOrder(supplier, created)
}

func startThreeProcessUAT(t *testing.T) *threeProcessUAT {
	t.Helper()
	pki := integration.NewGRPCTestPKI(t, "shop-user", "shop-supplier", "shop-order")
	redisPrefix := "core:test:06:three-process-uat:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	user, supplier, order := startShopProcesses(t, pki, redisPrefix)
	t.Cleanup(user.Stop)
	t.Cleanup(supplier.Stop)
	t.Cleanup(order.Stop)
	processes := []*integration.Suite{user, supplier, order}
	waitProcessReady(t, user, "/api/health", processes...)
	waitProcessReady(t, supplier, "/api/health", processes...)
	waitProcessReady(t, order, "/api/health", processes...)
	waitProcessReady(t, user, "/api/shop-user/getproducts", processes...)
	return &threeProcessUAT{
		t:        t,
		user:     user,
		supplier: supplier,
		order:    order,
		suffix:   strconv.FormatInt(time.Now().UnixNano(), 10),
	}
}

func scenarioTest(scenario *threeProcessUAT) *testing.T {
	scenario.t.Helper()
	return scenario.t
}
