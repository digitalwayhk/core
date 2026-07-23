package mq_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
)

type factoryTestProvider struct{ name string }

func (p *factoryTestProvider) Name() string                { return p.name }
func (*factoryTestProvider) Connect(context.Context) error { return nil }
func (*factoryTestProvider) Close() error                  { return nil }
func (*factoryTestProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	return nil
}
func (*factoryTestProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
func (*factoryTestProvider) Health(context.Context) error { return nil }

func TestBuildManager_ModeOnReturnsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cfg := &config.MQConfig{
		Mode:     "on",
		Provider: "redis-stream",
		Usage:    []string{"event-stream"},
	}
	cfg.ApplyDefaults()
	cfg.RedisStream.Addr = "127.0.0.1:0"

	mgr, err := mq.BuildManager(ctx, cfg)
	require.Error(t, err)
	assert.Nil(t, mgr)
}

func TestBuildManager_AutoDegradesOnProviderError(t *testing.T) {
	var output bytes.Buffer
	previous := logx.Reset()
	logx.SetWriter(logx.NewWriter(&output))
	t.Cleanup(func() {
		logx.Reset()
		if previous != nil {
			logx.SetWriter(previous)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	cfg := &config.MQConfig{
		Mode:     "auto",
		Provider: "redis-stream",
	}
	cfg.ApplyDefaults()
	cfg.RedisStream.Addr = "127.0.0.1:0"

	mgr, err := mq.BuildManager(ctx, cfg)
	require.NoError(t, err)
	assert.Nil(t, mgr)
	assert.Contains(t, output.String(), "mq_degraded")
	assert.Contains(t, output.String(), "redis-stream")
	assert.True(t, strings.Contains(output.String(), `"level":"info"`) || strings.Contains(output.String(), `"level": "info"`))
}

func TestBuildManager_RegisteredCustomProviderBuilds(t *testing.T) {
	const providerName = "factory-test-custom"
	mq.RegisterProviderFactory(providerName, func(context.Context, *config.MQConfig) (mq.MQProvider, error) {
		return &factoryTestProvider{name: providerName}, nil
	})
	t.Cleanup(func() { mq.UnregisterProviderFactory(providerName) })
	cfg := &config.MQConfig{Mode: "on", Provider: providerName, Usage: []string{"event-stream"}}
	require.NoError(t, cfg.Validate())

	mgr, err := mq.BuildManager(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, mgr)
	assert.Equal(t, providerName, mgr.Current().Name())
	require.NoError(t, mgr.Close())
}

func TestBuildManager_UnknownProviderIsHardConfigurationError(t *testing.T) {
	for _, mode := range []string{"auto", "on"} {
		t.Run(mode, func(t *testing.T) {
			mgr, err := mq.BuildManager(context.Background(), &config.MQConfig{Mode: mode, Provider: "factory-test-unknown"})
			require.Error(t, err)
			assert.ErrorIs(t, err, mq.ErrProviderConfiguration)
			assert.Contains(t, err.Error(), "register a provider factory")
			assert.Nil(t, mgr)
		})
	}
}

func TestBuildManager_UnimplementedBuiltinIsHardConfigurationError(t *testing.T) {
	for _, mode := range []string{"auto", "on"} {
		t.Run(mode, func(t *testing.T) {
			mgr, err := mq.BuildManager(context.Background(), &config.MQConfig{Mode: mode, Provider: "rabbitmq"})
			require.Error(t, err)
			assert.ErrorIs(t, err, mq.ErrProviderConfiguration)
			assert.Contains(t, err.Error(), "not implemented")
			assert.Nil(t, mgr)
		})
	}
}

func TestBuildManager_AutoOnlyDegradesTypedUnavailableError(t *testing.T) {
	const providerName = "factory-test-failing"
	factoryErr := errors.New("bad plugin configuration")
	mq.RegisterProviderFactory(providerName, func(context.Context, *config.MQConfig) (mq.MQProvider, error) {
		return nil, factoryErr
	})
	t.Cleanup(func() { mq.UnregisterProviderFactory(providerName) })

	mgr, err := mq.BuildManager(context.Background(), &config.MQConfig{Mode: "auto", Provider: providerName})
	require.ErrorIs(t, err, factoryErr)
	assert.Nil(t, mgr)
}

func TestUnregisterProviderFactory_RemovesFactory(t *testing.T) {
	const providerName = "factory-test-unregister"
	mq.RegisterProviderFactory(providerName, func(context.Context, *config.MQConfig) (mq.MQProvider, error) {
		return &factoryTestProvider{name: providerName}, nil
	})
	t.Cleanup(func() { mq.UnregisterProviderFactory(providerName) })
	mq.UnregisterProviderFactory(providerName)

	mgr, err := mq.BuildManager(context.Background(), &config.MQConfig{Mode: "on", Provider: providerName})
	require.Error(t, err)
	assert.ErrorIs(t, err, mq.ErrProviderConfiguration)
	assert.Nil(t, mgr)
}
