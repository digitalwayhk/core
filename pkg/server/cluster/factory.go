package cluster

import (
	"fmt"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/zeromicro/go-zero/core/logx"
)

// BuildProvider creates a DiscoveryProvider from the cluster configuration.
// sharedLocal is the process-level LocalProvider used both for intra-process
// MachineID claiming and as the fallback/default provider for local mode.
func BuildProvider(cfg *config.ClusterConfig, sharedLocal *LocalProvider) (DiscoveryProvider, error) {
	if cfg == nil || cfg.Mode == "off" {
		return nil, nil
	}

	switch cfg.Provider {
	case "", "local":
		return sharedLocal, nil
	case "etcd":
		p, err := NewEtcdProviderWithPrefix(cfg.Providers.Etcd.Endpoints, cfg.Providers.Etcd.TTL, cfg.Providers.Etcd.Prefix)
		if err != nil {
			if cfg.Mode == "on" {
				return nil, fmt.Errorf("cluster: etcd required but unavailable: %w", err)
			}
			logx.Infow("cluster_degraded",
				logx.Field("provider", "etcd"),
				logx.Field("fallback_provider", "local"),
				logx.Field("error", err),
			)
			return sharedLocal, nil
		}
		return p, nil
	case "consul":
		p, err := NewConsulProvider(cfg.Providers.Consul.Address)
		if err != nil {
			if cfg.Mode == "on" {
				return nil, fmt.Errorf("cluster: consul required but unavailable: %w", err)
			}
			logx.Infow("cluster_degraded",
				logx.Field("provider", "consul"),
				logx.Field("fallback_provider", "local"),
				logx.Field("error", err),
			)
			return sharedLocal, nil
		}
		return p, nil
	case "redis":
		p, err := NewRedisProvider(
			cfg.Providers.Redis.Addr,
			cfg.Providers.Redis.DB,
			cfg.Providers.Redis.Prefix,
			cfg.Providers.Redis.TTL,
		)
		if err != nil {
			if cfg.Mode == "on" {
				return nil, fmt.Errorf("cluster: redis required but unavailable: %w", err)
			}
			logx.Infow("cluster_degraded",
				logx.Field("provider", "redis"),
				logx.Field("fallback_provider", "local"),
				logx.Field("error", err),
			)
			return sharedLocal, nil
		}
		return p, nil
	default:
		return nil, fmt.Errorf("cluster: unknown provider %q", cfg.Provider)
	}
}
