package public

import (
	"errors"
	"net"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/transport"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// TransportStats exposes the transport counters owned by the ServiceContext
// that accepted the request. It is a local server-management diagnostic route.
type TransportStats struct {
	api.ServerArgs
}

type TransportStatsResponse struct {
	Transport transport.StatsSnapshot `json:"transport"`
	Fallback  []string                `json:"fallback"`
}

func (*TransportStats) Parse(types.IRequest) error { return nil }

func (own *TransportStats) Validation(req types.IRequest) error {
	context := router.GetContext(req.ServiceName())
	if context != nil {
		if option := context.GetServerOption(); option != nil && option.RemoteAccessManageAPI {
			return nil
		}
	}
	ip := net.ParseIP(req.GetClientIP())
	if ip == nil || !ip.IsLoopback() {
		return errors.New("transport stats are only available from loopback")
	}
	return nil
}

func (*TransportStats) Do(req types.IRequest) (interface{}, error) {
	bound, ok := req.(interface {
		GetService() *router.ServiceContext
	})
	if !ok || bound.GetService() == nil {
		return nil, errors.New("transport stats require a bound service context")
	}
	sc := bound.GetService()
	response := &TransportStatsResponse{}
	if sc.TransportStats != nil {
		response.Transport = sc.TransportStats.Snapshot()
	}
	if sc.Config != nil {
		response.Fallback = append([]string(nil), sc.Config.Transport.Fallback...)
	}
	return response, nil
}

func (own *TransportStats) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own, withSystemEndpointRateLimit())
}
