package cluster_test

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildProvider_Off(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := cluster.BuildProvider(&config.ClusterConfig{Mode: "off"}, sharedLocal)
	require.NoError(t, err)
	assert.Nil(t, provider)
}

func TestBuildProvider_Local(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := cluster.BuildProvider(&config.ClusterConfig{Mode: "auto", Provider: "local"}, sharedLocal)
	require.NoError(t, err)
	assert.Same(t, sharedLocal, provider)
}

func TestBuildProvider_AutoFallback(t *testing.T) {
	sharedLocal := cluster.NewLocalProvider(time.Second, time.Second, time.Second)
	provider, err := cluster.BuildProvider(&config.ClusterConfig{Mode: "auto", Provider: "etcd"}, sharedLocal)
	require.NoError(t, err)
	assert.Same(t, sharedLocal, provider)
}
