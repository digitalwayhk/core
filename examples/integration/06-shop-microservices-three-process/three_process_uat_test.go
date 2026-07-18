// 本文件编排 06 三进程 UAT 的完整业务主流程。
// 主流程只组合买家、供应商和管理员三个角色文件中的步骤，
// 让每个角色的功能闭环与异常权限断言可以独立阅读和维护。
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

// TestThreeProcessUATThreeRolesOrderVisibility 验证三角色在三进程部署下完成资料维护、商品上架、支付配置、下单和订单可见性隔离。
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
