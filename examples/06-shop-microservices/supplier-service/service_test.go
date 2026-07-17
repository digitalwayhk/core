package supplierservice

import (
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestSupplierServiceExposesManageAndConstrainedPublicRoutesOnly(t *testing.T) {
	routers := (&Service{}).Routers()
	require.NotEmpty(t, routers)

	for _, api := range routers {
		info := api.RouterInfo()
		require.NotEqual(t, servertypes.PrivateType, info.GetPathType(), info.GetPath())
		require.NotContains(t, info.GetPackPath(), "/api/call")
	}

	suppliers := (&publicapi.GetSuppliers{}).RouterInfo()
	require.Equal(t, []string{contract.UserServiceName}, suppliers.GetInternalCallers())

	products := (&publicapi.GetProducts{}).RouterInfo()
	require.Equal(t, []string{contract.OrderServiceName, contract.UserServiceName}, products.GetInternalCallers())
}
