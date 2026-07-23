package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestHandleResponseUsesTypedPublicErrorContract(t *testing.T) {
	tests := []struct {
		kind   types.ErrorKind
		status int
	}{
		{types.ErrorKindValidation, 400},
		{types.ErrorKindUnauthenticated, 401},
		{types.ErrorKindForbidden, 403},
		{types.ErrorKindNotFound, 404},
		{types.ErrorKindConflict, 409},
		{types.ErrorKindBusiness, 422},
		{types.ErrorKindRateLimited, 429},
		{types.ErrorKindUnavailable, 503},
		{types.ErrorKindInternal, 500},
	}
	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			cause := errors.New("database password=private-secret")
			err := fmt.Errorf("operation failed: %w", types.NewPublicError(tt.kind, 0, "", cause))
			res := (&router.InitRequest{}).NewResponse(nil, err)
			recorder := httptest.NewRecorder()

			HandleResponse(recorder, res)

			require.Equal(t, tt.status, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "private-secret")
			var body router.Response
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.False(t, body.Success)
			require.NotEmpty(t, body.ErrorMessage)
		})
	}
}

func TestHandleResponseUnknownErrorFailsClosed(t *testing.T) {
	res := (&router.InitRequest{}).NewResponse(nil, errors.New("not found token password=secret"))
	recorder := httptest.NewRecorder()

	HandleResponse(recorder, res)

	require.Equal(t, 500, recorder.Code)
	require.NotContains(t, recorder.Body.String(), "secret")
	require.Contains(t, recorder.Body.String(), "internal server error")
}

func TestHandleResponsePreservesLegacyTypeErrorCode(t *testing.T) {
	err := types.NewTypeError("orders", "/api/orders/create", "validation", "private validation detail", 700)
	res := (&router.InitRequest{}).NewResponse(nil, err)
	recorder := httptest.NewRecorder()

	HandleResponse(recorder, res)

	require.Equal(t, 400, recorder.Code)
	var body router.Response
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, 700, body.ErrorCode)
	require.Equal(t, "invalid request", body.ErrorMessage)
}
