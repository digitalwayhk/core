package runtime_test

import (
	"strings"
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

func TestRuntimePromQLExcludesManageControlPlaneFromBusinessMetrics(t *testing.T) {
	serviceQueries := []func(string, string) (string, error){
		runtime.ServiceRequestRateQuery,
		runtime.ServiceHTTPRateByCodeQuery,
		runtime.ServiceCoreRateByResultQuery,
		runtime.ServiceHTTPP50Query,
		runtime.ServiceHTTPP95Query,
		runtime.ServiceHTTPP99Query,
		runtime.ServiceRouteRateQuery,
		runtime.ServiceHTTPRouteRateQuery,
		runtime.ServiceCallP95Query,
	}
	for _, build := range serviceQueries {
		query, err := build("shop-order", "5m")
		require.NoError(t, err)
		require.Contains(t, query, `!~"^/api/(manage|servermanage)(/.*)?$"`, query)
	}

	lastSample, err := runtime.ServiceLastSampleTimestampQuery("shop-order")
	require.NoError(t, err)
	require.Equal(t, 2, strings.Count(lastSample, `!~"^/api/(manage|servermanage)(/.*)?$"`))

	edges, err := runtime.ServiceCallEdgeRateQuery("5m")
	require.NoError(t, err)
	require.Contains(t, edges, `target_route!~"^/api/(manage|servermanage)(/.*)?$"`)
}
