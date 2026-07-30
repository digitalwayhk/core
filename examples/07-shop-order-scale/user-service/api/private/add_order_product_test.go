package private

import (
	"net/http"
	"testing"

	supplierdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/supplier"
	supplierapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type unavailableProductRequest struct {
	*router.InitRequest
}

func newUnavailableProductRequest() *unavailableProductRequest {
	return &unavailableProductRequest{InitRequest: &router.InitRequest{}}
}

func (*unavailableProductRequest) GetUser() (string, string) {
	return "12345", ""
}

func (*unavailableProductRequest) CallService(
	target servertypes.IRouter,
	_ ...func(res servertypes.IResponse),
) (servertypes.IResponse, error) {
	switch target.(type) {
	case *supplierapi.GetProducts:
		return &router.Response{
			Success: true,
			Data:    []*supplierdto.Product{},
		}, nil
	default:
		return &router.Response{
			Success:      false,
			ErrorMessage: "unexpected downstream service",
		}, nil
	}
}

func TestAddOrderReportsUnavailableProduct(t *testing.T) {
	api := &AddOrder{
		ProductID: 831850452875269,
		Quantity:  130,
		RequestID: "string1",
	}

	_, err := api.Do(newUnavailableProductRequest())

	contract := servertypes.ResolvePublicError(err)
	require.Equal(t, servertypes.ErrorKindNotFound, contract.Kind)
	require.Equal(t, http.StatusNotFound, contract.HTTPStatus)
	require.Equal(t, "商品不存在或不可下单", contract.Message)
}
