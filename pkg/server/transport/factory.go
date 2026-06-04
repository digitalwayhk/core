package transport

import (
	"fmt"

	"github.com/digitalwayhk/core/pkg/server/config"
	grpctransport "github.com/digitalwayhk/core/pkg/server/transport/grpc"
	httptransport "github.com/digitalwayhk/core/pkg/server/transport/http"
	sockettransport "github.com/digitalwayhk/core/pkg/server/transport/socket"
	"github.com/zeromicro/go-zero/core/logx"
)

// BuildSelector constructs a TransportSelector from the given configuration,
// ordering transports according to Internal (primary) and Fallback list.
//
// Return values:
//   - (nil, nil)   — no transport configured (empty cfg); caller uses legacy HTTP path.
//   - (sel, nil)   — selector built successfully; unimplemented fallback entries were
//     warned and skipped.
//   - (nil, error) — Internal names an unimplemented protocol, or no implemented
//     transports could be built at all.
//
// Supported protocols: grpc, http, socket.
// Protocols "quic" and "mq" are recognised as valid config values but adapters are
// not yet implemented; using either as Internal returns an error.
func BuildSelector(cfg config.TransportConfig) (TransportSelector, error) {
	builders := map[string]func() Transport{
		"grpc": func() Transport {
			return grpctransport.New(cfg.GRPC.MaxRecvMsgSize, cfg.GRPC.MaxSendMsgSize)
		},
		"http":   func() Transport { return httptransport.New() },
		"socket": func() Transport { return sockettransport.New() },
	}

	if cfg.Internal == "" && len(cfg.Fallback) == 0 {
		return nil, nil
	}

	// Internal is the explicit primary choice — fail fast if not implemented.
	if cfg.Internal != "" {
		if _, ok := builders[cfg.Internal]; !ok {
			return nil, fmt.Errorf("transport: protocol %q not implemented; supported protocols: grpc, http, socket", cfg.Internal)
		}
	}

	order := make([]string, 0, 1+len(cfg.Fallback))
	if cfg.Internal != "" {
		order = append(order, cfg.Internal)
	}
	order = append(order, cfg.Fallback...)

	seen := make(map[string]bool)
	var transports []Transport
	for _, name := range order {
		if seen[name] || name == "" {
			continue
		}
		seen[name] = true
		if build, ok := builders[name]; ok {
			transports = append(transports, build())
		} else {
			// Fallback entry is unimplemented — warn but continue with the rest.
			logx.Errorf("transport: fallback protocol %q not implemented, skipping", name)
		}
	}

	if len(transports) == 0 {
		return nil, fmt.Errorf("transport: no implemented transports configured (all entries in fallback list are unimplemented)")
	}
	return NewDefaultSelector(transports[0], transports[1:]...), nil
}
