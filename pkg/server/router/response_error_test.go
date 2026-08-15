package router

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestResponseGetErrorDoesNotMutatePublicFields(t *testing.T) {
	err := types.NewTypeError("orders", "/api/orders/create", "validation", "database password=private", 700)
	response := &Response{
		err:          err,
		ErrorCode:    700,
		ErrorMessage: "invalid request",
	}

	require.ErrorIs(t, response.GetError(), err)
	require.Equal(t, 700, response.ErrorCode)
	require.Equal(t, "invalid request", response.ErrorMessage)
}

func TestNewResponseIsSafeBeforeRESTSerialization(t *testing.T) {
	err := types.NewTypeError("orders", "/api/orders/create", "validation", "database password=private", 700)
	response := (&Request{}).NewResponse(nil, err)

	body, marshalErr := json.Marshal(response)
	require.NoError(t, marshalErr)
	require.NotContains(t, string(body), "password")
	require.Contains(t, string(body), "invalid request")
}

func TestResponseRoundTripPreservesDownstreamPublicError(t *testing.T) {
	publicErr := types.NewPublicError(
		types.ErrorKindBusiness,
		types.PublicCodeBusiness,
		"订单数量超过最大下单数量",
		errors.New("quantity exceeds configured maximum"),
	)
	downstreamErr := types.NewTypeErrorWithCause(
		"shop-order",
		"/api/shop-order/createorder",
		"do",
		"internal operation detail",
		800,
		publicErr,
	)
	original := (&Request{}).NewResponse(nil, downstreamErr)
	body, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Response
	require.NoError(t, json.Unmarshal(body, &decoded))

	contract := types.ResolvePublicError(decoded.GetError())
	require.Equal(t, types.ErrorKindBusiness, contract.Kind)
	require.Equal(t, 800, contract.Code)
	require.Equal(t, 422, contract.HTTPStatus)
	require.Equal(t, "订单数量超过最大下单数量", contract.Message)
}
