package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeObservabilityDefaults(t *testing.T) {
	var c RuntimeObservabilityConfig
	c.ApplyDefaults()
	require.Equal(t, "off", c.Mode)
	require.Equal(t, 3*time.Second, c.QueryTimeout)
	require.Equal(t, 4, c.MaxConcurrentQueries)
	require.Equal(t, 5*time.Second, c.CacheTTL)
	require.NoError(t, c.Validate())
}

func TestRuntimeObservabilityValidatePrometheusRequiresURL(t *testing.T) {
	c := RuntimeObservabilityConfig{Mode: "prometheus"}
	c.ApplyDefaults()
	require.ErrorContains(t, c.Validate(), "QueryURL")
}

func TestRuntimeObservabilityValidateRejectsBadMode(t *testing.T) {
	c := RuntimeObservabilityConfig{Mode: "memory"}
	c.ApplyDefaults()
	require.ErrorContains(t, c.Validate(), "Mode")
}

func TestRuntimeObservabilityValidateAcceptsPrometheusURL(t *testing.T) {
	c := RuntimeObservabilityConfig{
		Mode:     "prometheus",
		QueryURL: "http://prometheus:9090",
	}
	c.ApplyDefaults()
	require.NoError(t, c.Validate())
}

func TestRuntimeObservabilityValidateRejectsBadURL(t *testing.T) {
	c := RuntimeObservabilityConfig{
		Mode:     "prometheus",
		QueryURL: "://bad",
	}
	c.ApplyDefaults()
	require.Error(t, c.Validate())
}

func TestServerConfigValidateIncludesRuntimeObservability(t *testing.T) {
	cfg := NewServiceDefaultConfig("demo", 8080)
	cfg.RuntimeObservability.Mode = "prometheus"
	cfg.RuntimeObservability.QueryURL = "://bad"
	cfg.RuntimeObservability.ApplyDefaults()
	require.Error(t, cfg.Validate())

	cfg.RuntimeObservability.QueryURL = "http://127.0.0.1:9090"
	require.NoError(t, cfg.Validate())
}

func TestServerConfigDefaultRuntimeObservabilityOff(t *testing.T) {
	cfg := NewServiceDefaultConfig("demo", 8080)
	require.Equal(t, "off", cfg.RuntimeObservability.Mode)
	require.NoError(t, cfg.Validate())
}
