package mq

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRedisMessageLifecycleConformance(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 设置 CORE_TEST_REDIS_ADDR 后运行 Redis lifecycle conformance")
	}
	prefix := fmt.Sprintf("core:test:conformance:%d", time.Now().UnixNano())
	provider := NewRedisStreamProvider(addr, prefix, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	require.NoError(t, provider.Connect(ctx))
	defer provider.Close()

	require.NoError(t, VerifyMessageLifecycleConformance(ctx, provider, "fills"))
}

func TestNATSMessageLifecycleConformance(t *testing.T) {
	provider, parent := newNATSReliableProvider(t)
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	defer cancel()

	require.NoError(t, VerifyMessageLifecycleConformance(ctx, provider, "conformance.fills"))
}
