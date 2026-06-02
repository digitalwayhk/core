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
		p, err := NewEtcdProvider(cfg.Providers.Etcd.Endpoints)
		if err != nil {
			if cfg.Mode == "on" {
				return nil, fmt.Errorf("cluster: etcd required but unavailable: %w", err)
			}
			logx.Errorf("cluster: etcd unavailable, falling back to local: %v", err)
			return sharedLocal, nil
		}
		return p, nil
	case "consul":
		p, err := NewConsulProvider(cfg.Providers.Consul.Address)
		if err != nil {
			if cfg.Mode == "on" {
				return nil, fmt.Errorf("cluster: consul required but unavailable: %w", err)
			}
			logx.Errorf("cluster: consul unavailable, falling back to local: %v", err)
			return sharedLocal, nil
		}
		return p, nil
	default:
		logx.Errorf("cluster: unknown provider %q, using local", cfg.Provider)
		return sharedLocal, nil
	}
}
