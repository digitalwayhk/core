// Package shoporderscalemultiprocess 提供 07 Docker UAT 的角色级数据准备能力。
// 买家、供应商和管理员角色测试必须通过这些角色 fixture 组合关键数据，避免在单个角色测试中临时伪造其他角色的数据。
package shoporderscalemultiprocess

import (
	"testing"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/stretchr/testify/require"
)

type dockerAdminFixture struct {
	Token         string
	PaymentTypeID uint
}

type dockerSupplierFixture struct {
	Token     string
	ProductID uint
}

func prepareDockerAdminFixture(t *testing.T, supplier *integration.Suite) dockerAdminFixture {
	t.Helper()
	fixture := dockerAdminFixture{
		Token:         supplier.TokenFor(t, "900001", 1),
		PaymentTypeID: 1,
	}
	require.NotEmpty(t, fixture.Token)
	require.NotZero(t, fixture.PaymentTypeID)
	return fixture
}

func prepareDockerSupplierFixture(t *testing.T, supplier *integration.Suite, _ dockerAdminFixture) dockerSupplierFixture {
	t.Helper()
	fixture := dockerSupplierFixture{
		Token: supplier.TokenFor(t, "920002", 1),
	}
	fixture.ProductID = addDockerSupplierProduct(t, supplier, fixture.Token)
	require.NotZero(t, fixture.ProductID)
	return fixture
}
