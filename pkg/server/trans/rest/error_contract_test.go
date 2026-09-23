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
	require.Equal(t, types.PublicCodeValidation, body.ErrorCode)
	require.Equal(t, "invalid request", body.ErrorMessage)
}

type execDoErrorRouter struct {
	err error
}

func (*execDoErrorRouter) Parse(types.IRequest) error      { return nil }
func (*execDoErrorRouter) Validation(types.IRequest) error { return nil }
func (router *execDoErrorRouter) Do(types.IRequest) (interface{}, error) {
	return nil, router.err
}
func (*execDoErrorRouter) RouterInfo() *types.RouterInfo { return nil }

// TestExecDoToHTTPResponsePreservesPublicContract 覆盖真实 TypeError 包装链到最终 HTTP 响应。
func TestExecDoToHTTPResponsePreservesPublicContract(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		status    int
		code      int
		message   string
		forbidden string
	}{
		{
			name:      "unclassified do error",
			err:       errors.New("Duplicate entry secret-index"),
			status:    500,
			code:      types.PublicCodeInternal,
			message:   "internal server error",
			forbidden: "secret-index",
		},
		{
			name:      "classified conflict",
			err:       types.NewPublicError(types.ErrorKindConflict, 40917, "record already exists", errors.New("Duplicate entry secret-index")),
			status:    409,
			code:      40917,
			message:   "record already exists",
			forbidden: "secret-index",
		},
		{
			name:      "classified business",
			err:       types.NewPublicError(types.ErrorKindBusiness, 42217, "order limit exceeded", errors.New("private business detail")),
			status:    422,
			code:      42217,
			message:   "order limit exceeded",
			forbidden: "private business detail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &types.RouterInfo{Path: "/api/test/error", ServiceName: "test"}
			response := info.ExecDo(&execDoErrorRouter{err: tt.err}, &router.InitRequest{})
			recorder := httptest.NewRecorder()

			HandleResponse(recorder, response)

			require.Equal(t, tt.status, recorder.Code)
			var body router.Response
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
			require.Equal(t, tt.code, body.ErrorCode)
			require.Equal(t, tt.message, body.ErrorMessage)
			require.NotContains(t, recorder.Body.String(), tt.forbidden)
		})
	}
}
