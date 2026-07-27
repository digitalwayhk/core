package runtime_test

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/runtime"
	"github.com/stretchr/testify/require"
)

func TestPromQLServiceRateUsesAllowlistedWindow(t *testing.T) {
	q, err := runtime.ServiceRequestRateQuery("shop-order", "15s")
	require.NoError(t, err)
	require.Contains(t, q, `service="shop-order"`)
	require.Contains(t, q, `[15s]`)
}

func TestPromQLRejectsUnknownWindow(t *testing.T) {
	_, err := runtime.ServiceRequestRateQuery("shop-order", "7d")
	require.Error(t, err)
}

func TestPromQLRejectsUnsafeServiceName(t *testing.T) {
	_, err := runtime.ServiceRequestRateQuery(`shop-order",on`, "15s")
	require.Error(t, err)
}
