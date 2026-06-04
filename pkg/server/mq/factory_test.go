package mq_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
}
