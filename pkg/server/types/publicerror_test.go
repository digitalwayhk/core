package types

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicErrorPreservesCauseAndStableContract(t *testing.T) {
	cause := errors.New("sql password=secret")
	err := NewPublicError(ErrorKindConflict, 40942, "resource conflict", cause)
	wrapped := fmt.Errorf("save order: %w", err)
	joined := errors.Join(errors.New("secondary failure"), wrapped)

	require.ErrorIs(t, joined, cause)
	var publicErr *PublicError
	require.ErrorAs(t, joined, &publicErr)
	contract := ResolvePublicError(joined)
	require.Equal(t, ErrorKindConflict, contract.Kind)
	require.Equal(t, 40942, contract.Code)
	require.Equal(t, 409, contract.HTTPStatus)
	require.Equal(t, "resource conflict", contract.Message)
}

func TestTypeErrorWithCausePreservesErrorChain(t *testing.T) {
	cause := errors.New("decoder detail")
	err := NewTypeErrorWithCause("orders", "/api/orders/create", "parse", "parse failed", 600, cause)
	require.ErrorIs(t, err, cause)
	contract := ResolvePublicError(err)
	require.Equal(t, ErrorKindValidation, contract.Kind)
	require.Equal(t, PublicCodeValidation, contract.Code)
	require.Equal(t, 400, contract.HTTPStatus)
	require.Equal(t, "invalid request", contract.Message)
}

func TestTypeErrorPreservesCompletePublicCauseContract(t *testing.T) {
	originalCause := errors.New("quantity exceeds configured maximum")
	cause := NewPublicError(ErrorKindBusiness, 42217, "订单数量必须大于 0", originalCause)
	err := NewTypeErrorWithCause("shop", "/api/shop/addorder", "do", "internal operation detail", 800, cause)

	contract := ResolvePublicError(err)
	require.Equal(t, ErrorKindBusiness, contract.Kind)
	require.Equal(t, 42217, contract.Code)
	require.Equal(t, 422, contract.HTTPStatus)
	require.Equal(t, "订单数量必须大于 0", contract.Message)
	require.ErrorIs(t, err, originalCause)
}

func TestTypeErrorPreservesAllClassifiedPublicErrors(t *testing.T) {
	tests := []struct {
		name    string
		kind    ErrorKind
		code    int
		status  int
		message string
	}{
		{name: "conflict", kind: ErrorKindConflict, code: 40917, status: 409, message: "record already exists"},
		{name: "not found", kind: ErrorKindNotFound, code: 40417, status: 404, message: "record not found"},
		{name: "rate limited", kind: ErrorKindRateLimited, code: 42917, status: 429, message: "try again later"},
		{name: "unavailable", kind: ErrorKindUnavailable, code: 50317, status: 503, message: "dependency unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cause := NewPublicError(tt.kind, tt.code, tt.message, errors.New("private detail"))
			err := NewTypeErrorWithCause("shop", "/api/shop/action", "do", "internal operation detail", 800, cause)

			contract := ResolvePublicError(err)
			require.Equal(t, tt.kind, contract.Kind)
			require.Equal(t, tt.code, contract.Code)
			require.Equal(t, tt.status, contract.HTTPStatus)
			require.Equal(t, tt.message, contract.Message)
		})
	}
}

func TestTypeErrorDoWithUnclassifiedCauseFailsClosed(t *testing.T) {
	cause := errors.New("duplicate key database password=private")
	err := NewTypeErrorWithCause("shop", "/api/shop/addorder", "do", "internal operation detail", 800, cause)

	contract := ResolvePublicError(err)
	require.Equal(t, ErrorKindInternal, contract.Kind)
	require.Equal(t, PublicCodeInternal, contract.Code)
	require.Equal(t, 500, contract.HTTPStatus)
	require.Equal(t, "internal server error", contract.Message)
	require.NotContains(t, contract.Message, "business rule rejected")
	require.ErrorIs(t, err, cause)
}

func TestTypeErrorValidationWithUnclassifiedCauseIsSafe(t *testing.T) {
	cause := errors.New("invalid field database password=private")
	err := NewTypeErrorWithCause("shop", "/api/shop/addorder", "validation", "internal validation detail", 700, cause)

	contract := ResolvePublicError(err)
	require.Equal(t, ErrorKindValidation, contract.Kind)
	require.Equal(t, PublicCodeValidation, contract.Code)
	require.Equal(t, 400, contract.HTTPStatus)
	require.Equal(t, "invalid request", contract.Message)
	require.NotContains(t, contract.Message, "password")
	require.ErrorIs(t, err, cause)
}

func TestResolvePublicErrorDoesNotClassifyByMessage(t *testing.T) {
	for _, message := range []string{"not found", "unauthorized token", "业务失败", "database exploded"} {
		contract := ResolvePublicError(errors.New(message))
		require.Equal(t, ErrorKindInternal, contract.Kind)
		require.Equal(t, 500, contract.HTTPStatus)
		require.Equal(t, PublicCodeInternal, contract.Code)
	}
}

// TestPayloadTooLargePublicErrorContract 验证请求体超限使用独立的 413 状态和稳定公开错误码。
func TestPayloadTooLargePublicErrorContract(t *testing.T) {
	contract := NewPublicError(ErrorKindPayloadTooLarge, 0, "", nil).PublicErrorContract()

	require.Equal(t, ErrorKindPayloadTooLarge, contract.Kind)
	require.Equal(t, PublicCodePayloadTooLarge, contract.Code)
	require.Equal(t, 413, contract.HTTPStatus)
	require.Equal(t, "request entity too large", contract.Message)
}

func TestLegacyTypeErrorStageCodesRemainInternalMetadata(t *testing.T) {
	tests := []struct {
		operation string
		kind      ErrorKind
		code      int
		status    int
	}{
		{operation: "parse", kind: ErrorKindValidation, code: PublicCodeValidation, status: 400},
		{operation: "validation", kind: ErrorKindValidation, code: PublicCodeValidation, status: 400},
		{operation: "do", kind: ErrorKindInternal, code: PublicCodeInternal, status: 500},
		{operation: "panic", kind: ErrorKindInternal, code: PublicCodeInternal, status: 500},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			stageCode := map[string]int{"parse": 600, "validation": 700, "do": 800, "panic": 500}[tt.operation]
			err := NewTypeError("orders", "/api/orders/create", tt.operation, "internal detail", stageCode)
			contract := ResolvePublicError(err)
			require.Equal(t, tt.code, contract.Code)
			require.Equal(t, tt.kind, contract.Kind)
			require.Equal(t, tt.status, contract.HTTPStatus)
		})
	}
}
