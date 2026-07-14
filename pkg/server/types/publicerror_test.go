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
	require.Equal(t, 600, ResolvePublicError(err).Code)
}

func TestTypeErrorPreservesSafePublicCauseMessageAndStageCode(t *testing.T) {
	cause := NewPublicError(ErrorKindValidation, PublicCodeValidation, "订单数量必须大于 0", errors.New("quantity is not positive"))
	err := NewTypeErrorWithCause("shop", "/api/shop/addorder", "validation", "internal validation detail", 700, cause)

	contract := ResolvePublicError(err)
	require.Equal(t, ErrorKindValidation, contract.Kind)
	require.Equal(t, 700, contract.Code)
	require.Equal(t, 400, contract.HTTPStatus)
	require.Equal(t, "订单数量必须大于 0", contract.Message)
}

func TestResolvePublicErrorDoesNotClassifyByMessage(t *testing.T) {
	for _, message := range []string{"not found", "unauthorized token", "业务失败", "database exploded"} {
		contract := ResolvePublicError(errors.New(message))
		require.Equal(t, ErrorKindInternal, contract.Kind)
		require.Equal(t, 500, contract.HTTPStatus)
		require.Equal(t, PublicCodeInternal, contract.Code)
	}
}

func TestLegacyTypeErrorStageCodesRemainStable(t *testing.T) {
	tests := []struct {
		operation string
		code      int
		kind      ErrorKind
		status    int
	}{
		{operation: "parse", code: 600, kind: ErrorKindValidation, status: 400},
		{operation: "validation", code: 700, kind: ErrorKindValidation, status: 400},
		{operation: "do", code: 800, kind: ErrorKindBusiness, status: 422},
		{operation: "panic", code: 500, kind: ErrorKindInternal, status: 500},
	}
	for _, tt := range tests {
		t.Run(tt.operation, func(t *testing.T) {
			err := NewTypeError("orders", "/api/orders/create", tt.operation, "internal detail", tt.code)
			contract := ResolvePublicError(err)
			require.Equal(t, tt.code, contract.Code)
			require.Equal(t, tt.kind, contract.Kind)
			require.Equal(t, tt.status, contract.HTTPStatus)
		})
	}
}
