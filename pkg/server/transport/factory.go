package transport

import (
	"github.com/digitalwayhk/core/pkg/server/config"
	grpctransport "github.com/digitalwayhk/core/pkg/server/transport/grpc"
	httptransport "github.com/digitalwayhk/core/pkg/server/transport/http"
	sockettransport "github.com/digitalwayhk/core/pkg/server/transport/socket"
	"github.com/zeromicro/go-zero/core/logx"
)

// BuildSelector constructs a TransportSelector from the given configuration,
// ordering transports according to Internal (primary) and Fallback list.
// Unknown or unimplemented protocols are skipped with a log warning.
// Returns nil if no valid transports are configured.
func BuildSelector(cfg config.TransportConfig) TransportSelector {
	builders := map[string]func() Transport{
		"grpc": func() Transport {
			return grpctransport.New(cfg.GRPC.MaxRecvMsgSize, cfg.GRPC.MaxSendMsgSize)
		},
		"http":   func() Transport { return httptransport.New() },
		"socket": func() Transport { return sockettransport.New() },
	}

	order := make([]string, 0, 1+len(cfg.Fallback))
	if cfg.Internal != "" {
		order = append(order, cfg.Internal)
	}
	order = append(order, cfg.Fallback...)

	seen := make(map[string]bool)
	var transports []Transport
	for _, name := range order {
		if seen[name] {
			continue
		}
		seen[name] = true
		if build, ok := builders[name]; ok {
			transports = append(transports, build())
		} else if name != "" {
			logx.Errorf("transport: protocol %q not implemented, skipping", name)
		}
	}

	if len(transports) == 0 {
		return nil
	}
	return NewDefaultSelector(transports[0], transports[1:]...)
}
