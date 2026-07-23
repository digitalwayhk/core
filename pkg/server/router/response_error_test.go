package router

import (
	"encoding/json"
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
