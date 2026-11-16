// 本文件在原PEL与正式Provider上验证滚动进展不会反复读取未执行尾项正文。
package mq

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSharedPendingRollingProgressKeepsHints 头完成后保留有界尾ID提示，旧PEL与实际ACK仍为权威。
func TestSharedPendingRollingProgressKeepsHints(t *testing.T) {
	p := newWindowRedisProvider(t)
	ctx := context.Background()
	hook := &pendingBodyReadHook{byID: map[string]int{}}
	p.client.AddHook(hook)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	for i := range 128 {
		require.NoError(t, p.Publish(ctx, "rolling", []byte(fmt.Sprint(i)), &PublishOptions{OrderingKey: fmt.Sprint(i % 4)}))
	}
	var healthy atomic.Int32
	stop, e := p.SubscribeReliable(ctx, "rolling", ReliableSubscribeOptions{Group: "g", Consumer: "one", SharedKeys: true, KeyConcurrency: 4, Count: 64}, func(m *Message) error {
		if string(m.Data) == "0" {
			<-release
		}
		if string(m.Data) != "0" {
			healthy.Add(1)
		}
		return nil
	})
	require.NoError(t, e)
	defer stop()
	require.Eventually(t, func() bool { return healthy.Load() >= 96 }, 5*time.Second, time.Millisecond)
	hook.mu.Lock()
	reads, maxReads := 0, 0
	for _, n := range hook.byID {
		reads += n
		if n > maxReads {
			maxReads = n
		}
	}
	hook.mu.Unlock()
	require.LessOrEqual(t, maxReads, 2, "每个尾项只应有发现与准入两次正文读取，不随其他头的完成反复重取")
	t.Logf("published=128 body_reads=%d max_per_id=%d", reads, maxReads)
	unblock()
	require.Eventually(t, func() bool { return p.client.XPending(ctx, p.streamKey("rolling"), "g").Val().Count == 0 }, 5*time.Second, time.Millisecond)
}
