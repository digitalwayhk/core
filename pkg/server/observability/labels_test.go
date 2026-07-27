package observability_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/stretchr/testify/require"
)

func TestNormalizeServiceLabel(t *testing.T) {
	require.Equal(t, "shop-order", observability.NormalizeServiceLabel(" Shop-Order "))
	require.Equal(t, "unknown", observability.NormalizeServiceLabel(""))
	require.Equal(t, "unknown", observability.NormalizeServiceLabel("shop order"))
}

func TestNormalizeRouteLabel(t *testing.T) {
	require.Equal(t, "/api/shop-order/addorder", observability.NormalizeRouteLabel("/api/shop-order/addorder"))
	require.Equal(t, "invalid_route", observability.NormalizeRouteLabel("/api/x?id=1"))
	require.Equal(t, "invalid_route", observability.NormalizeRouteLabel(""))
	require.Equal(t, "invalid_route", observability.NormalizeRouteLabel("api/x"))
}

func TestClassifyResult(t *testing.T) {
	require.Equal(t, observability.ResultSuccess, observability.ClassifyHTTPStatus(200))
	require.Equal(t, observability.ResultClientError, observability.ClassifyHTTPStatus(404))
	require.Equal(t, observability.ResultServerError, observability.ClassifyHTTPStatus(500))
	require.Equal(t, observability.ResultTimeout, observability.ClassifyError(context.DeadlineExceeded))
	require.Equal(t, observability.ResultClientError, observability.ClassifyError(context.Canceled))
}

func TestNormalizeProtocolAndResultClass(t *testing.T) {
	require.Equal(t, "grpc", observability.NormalizeProtocol(" gRPC "))
	require.Equal(t, "unknown", observability.NormalizeProtocol("quic"))
	require.Equal(t, observability.ResultRejected, observability.NormalizeResultClass("rejected"))
	require.Equal(t, observability.ResultUnavailable, observability.NormalizeResultClass("boom"))
}

func TestIsSafePromLabel(t *testing.T) {
	require.True(t, observability.IsSafePromLabel("shop-order"))
	require.False(t, observability.IsSafePromLabel(`shop-order",on`))
	require.False(t, observability.IsSafePromLabel("shop order"))
	require.False(t, observability.IsSafePromLabel(""))
}

func TestClassifyErrorTimeoutNet(t *testing.T) {
	err := context.DeadlineExceeded
	require.Equal(t, observability.ResultTimeout, observability.ClassifyError(err))
	// 保持未使用 time 的编译友好引用，避免误删后续扩展示例。
	_ = time.Millisecond
}
