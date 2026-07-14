package event

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceEventBridgeControlQueueTimeoutIsBoundedAndCounted(t *testing.T) {
	bridge := NewServiceEventBridge(NewStream(), ServiceEventBridgeOptions{
		ObserverQueueSize:     1,
		ControlQueueSize:      1,
		ControlShards:         1,
		ControlEnqueueTimeout: 20 * time.Millisecond,
	})
	var releaseOnce sync.Once
	release := make(chan struct{})
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		closeRelease()
		require.NoError(t, bridge.Close(context.Background()))
	})

	started := make(chan struct{})
	var handlerCalls atomic.Int32
	cancel, err := bridge.Subscribe("cache.invalidate", func(*Envelope) {
		if handlerCalls.Add(1) == 1 {
			close(started)
		}
		<-release
	})
	require.NoError(t, err)
	defer cancel()

	publish := func(value string) error {
		env := NewEnvelope("service-a", "cache.invalidate", []byte(value))
		env.ShardKey = "route-a"
		return bridge.Publish(context.Background(), PublishRequest{
			Class:    ControlDelivery,
			Envelope: env,
		})
	}

	firstResult := make(chan error, 1)
	go func() { firstResult <- publish("first") }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("首个控制事件未进入处理器")
	}

	secondResult := make(chan error, 1)
	go func() { secondResult <- publish("second") }()
	require.Eventually(t, func() bool {
		return len(bridge.controlQueues[0]) == 1
	}, time.Second, time.Millisecond)

	startedAt := time.Now()
	err = publish("third")
	assert.ErrorIs(t, err, ErrControlQueueTimeout)
	assert.Less(t, time.Since(startedAt), time.Second)
	assert.Equal(t, uint64(1), bridge.ControlQueueTimeouts())

	closeRelease()
	require.NoError(t, <-firstResult)
	require.NoError(t, <-secondResult)
	assert.Equal(t, int32(2), handlerCalls.Load(), "超时的第三个事件不得入队")
}
