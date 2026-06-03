package mq

import (
	"context"
	"fmt"
	"sync"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/zeromicro/go-zero/core/logx"
)

// ProviderFactory creates an MQProvider from configuration.
type ProviderFactory func(ctx context.Context, cfg *config.MQConfig) (MQProvider, error)

var (
	providerFactoriesMu sync.RWMutex
	providerFactories   = map[string]ProviderFactory{}
)

// RegisterProviderFactory registers a custom factory for the named provider.
// Registered factories take precedence over built-in switch cases, enabling
// test-only or plugin providers without modifying production code.
func RegisterProviderFactory(name string, factory ProviderFactory) {
	providerFactoriesMu.Lock()
	defer providerFactoriesMu.Unlock()
	providerFactories[name] = factory
}

// BuildManager creates and connects an MQManager from configuration.
func BuildManager(ctx context.Context, cfg *config.MQConfig) (*MQManager, error) {
	if cfg == nil || cfg.Mode == "off" {
		return nil, nil
	}

	provider, err := buildProvider(ctx, cfg)
	if err != nil {
		if cfg.Mode == "on" {
			return nil, err
		}
		logx.Errorf("mq: provider %q unavailable: %v", cfg.Provider, err)
		return nil, nil
	}

	mgr := NewManager()
	mgr.Register(provider)
	if err := mgr.SetCurrent(provider.Name()); err != nil {
		_ = provider.Close()
		return nil, err
	}
	return mgr, nil
}

func buildProvider(ctx context.Context, cfg *config.MQConfig) (MQProvider, error) {
	// Check registered factories first — enables test/plugin providers.
	providerFactoriesMu.RLock()
	factory, ok := providerFactories[cfg.Provider]
	providerFactoriesMu.RUnlock()
	if ok {
		return factory(ctx, cfg)
	}

	switch cfg.Provider {
	case "", "redis-stream":
		provider := NewRedisStreamProvider(cfg.RedisStream.Addr, cfg.RedisStream.Prefix, cfg.RedisStream.DB)
		if err := provider.Connect(ctx); err != nil {
			return nil, fmt.Errorf("mq: connect redis-stream: %w", err)
		}
		return provider, nil
	case "nats-jetstream":
		provider := NewNATSJetStreamProvider(
			cfg.NATSJetStream.URL,
			cfg.NATSJetStream.StreamPrefix,
			cfg.NATSJetStream.DurablePrefix,
		)
		if err := provider.Connect(ctx); err != nil {
			return nil, fmt.Errorf("mq: connect nats-jetstream: %w", err)
		}
		return provider, nil
	case "kafka", "rabbitmq", "rocketmq":
		return nil, fmt.Errorf("mq: provider %q is not implemented", cfg.Provider)
	default:
		return nil, fmt.Errorf("mq: unsupported provider %q", cfg.Provider)
	}
}
