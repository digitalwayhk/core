package public

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/stretchr/testify/require"
)

type transportStatsRequest struct {
	*publicRateLimitRequest
	service *router.ServiceContext
}

func (r *transportStatsRequest) GetService() *router.ServiceContext { return r.service }

func TestTransportStatsUsesRequestServiceContext(t *testing.T) {
	stats := &transport.Stats{}
	stats.RecordInboundGRPC()
	sc := &router.ServiceContext{
		Config:         &config.ServerConfig{Transport: config.TransportConfig{Fallback: []string{"http"}}},
		TransportStats: stats,
	}
	request := &transportStatsRequest{
		publicRateLimitRequest: &publicRateLimitRequest{clientIP: "127.0.0.1", serviceName: "wrong-global-name"},
		service:                sc,
	}

	result, err := (&TransportStats{}).Do(request)
	require.NoError(t, err)
	snapshot := result.(*TransportStatsResponse)
	require.Equal(t, uint64(1), snapshot.Transport.InboundGRPC)
	require.Equal(t, []string{"http"}, snapshot.Fallback)
}

func TestTransportStatsRejectsUnauthorisedRemoteAccess(t *testing.T) {
	request := &transportStatsRequest{
		publicRateLimitRequest: &publicRateLimitRequest{clientIP: "198.51.100.10", serviceName: "missing-service"},
		service:                &router.ServiceContext{},
	}
	require.Error(t, (&TransportStats{}).Validation(request))
}

func TestTransportStatsRouteIsLocalServerManageEndpoint(t *testing.T) {
	info := (&TransportStats{}).RouterInfo()
	require.Equal(t, "/api/servermanage/transportstats", info.GetPath())
	require.False(t, info.GetAuth())
}
