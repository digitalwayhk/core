package mq

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/zeromicro/go-zero/core/logx"
)

var (
	// ErrProviderConfiguration identifies provider selections that cannot be
	// constructed without changing configuration or registering a factory.
	ErrProviderConfiguration = errors.New("mq: provider configuration error")
	// ErrProviderUnavailable identifies a supported provider that could not
	// connect. Mode=auto may degrade only for this error class.
	ErrProviderUnavailable = errors.New("mq: provider unavailable")
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

// UnregisterProviderFactory removes a custom provider factory.
func UnregisterProviderFactory(name string) {
	providerFactoriesMu.Lock()
	defer providerFactoriesMu.Unlock()
	delete(providerFactories, name)
}

// BuildManager creates and connects an MQManager from configuration.
func BuildManager(ctx context.Context, cfg *config.MQConfig) (*MQManager, error) {
	if cfg == nil || cfg.Mode == "off" {
		return nil, nil
	}

	provider, err := buildProvider(ctx, cfg)
	if err != nil {
		if cfg.Mode == "auto" && errors.Is(err, ErrProviderUnavailable) {
			logx.Infow("mq_degraded",
				logx.Field("provider", cfg.Provider),
				logx.Field("error", err),
			)
			return nil, nil
		}
		return nil, err
	}
	if provider == nil {
		return nil, fmt.Errorf("%w: factory for %q returned nil provider", ErrProviderConfiguration, cfg.Provider)
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
		provider, err := factory(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("mq: provider factory %q: %w", cfg.Provider, err)
		}
		return provider, nil
	}

	switch cfg.Provider {
	case "", "redis-stream":
		provider := NewRedisStreamProvider(cfg.RedisStream.Addr, cfg.RedisStream.Prefix, cfg.RedisStream.DB)
		if err := provider.Connect(ctx); err != nil {
			return nil, fmt.Errorf("%w: connect redis-stream: %v", ErrProviderUnavailable, err)
		}
		return provider, nil
	case "nats-jetstream":
		provider := NewNATSJetStreamProvider(
			cfg.NATSJetStream.URL,
			cfg.NATSJetStream.StreamPrefix,
			cfg.NATSJetStream.DurablePrefix,
		)
		if err := provider.Connect(ctx); err != nil {
			return nil, fmt.Errorf("%w: connect nats-jetstream: %v", ErrProviderUnavailable, err)
		}
		return provider, nil
	case "kafka", "rabbitmq", "rocketmq":
		return nil, fmt.Errorf("%w: provider %q is not implemented; register a provider factory or choose redis-stream/nats-jetstream", ErrProviderConfiguration, cfg.Provider)
	default:
		return nil, fmt.Errorf("%w: provider %q is unknown; register a provider factory", ErrProviderConfiguration, cfg.Provider)
	}
}
