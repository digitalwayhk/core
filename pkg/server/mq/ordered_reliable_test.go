package mq_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

func TestFakeOrderedReliableSameKeyStrictOrderAndFailureBarrier(t *testing.T) {
	provider := mq.NewFakeOrderedReliableProvider()
	require.True(t, provider.OrderedReliableInfo().Valid())

	var (
		mu        sync.Mutex
		got       []string
		allowM3   atomic.Bool
		m3Fails   atomic.Int32
		completed = make(chan struct{})
	)
	cancel, err := provider.SubscribeReliable(context.Background(), "fills", mq.ReliableSubscribeOptions{Group: "positions"}, func(msg *mq.Message) error {
		body := string(msg.Data)
		if body == "m3" && !allowM3.Load() {
			m3Fails.Add(1)
			return errors.New("boom")
		}
		mu.Lock()
		got = append(got, body)
		n := len(got)
		mu.Unlock()
		if n == 5 {
			close(completed)
		}
		return nil
	})
	require.NoError(t, err)
	defer cancel()

	ctx := context.Background()
	for _, body := range []string{"m1", "m2", "m3", "m4", "m5"} {
		require.NoError(t, provider.Publish(ctx, "fills", []byte(body), &mq.PublishOptions{
			OrderingKey: "market-a", IdempotencyKey: body,
		}))
	}

	// 失败阻断窗口：m3 持续失败时，m4/m5 不得执行
	time.Sleep(120 * time.Millisecond)
	mu.Lock()
	require.Equal(t, []string{"m1", "m2"}, got)
	mu.Unlock()
	require.GreaterOrEqual(t, m3Fails.Load(), int32(1))

	allowM3.Store(true)
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		mu.Lock()
		t.Fatalf("timeout got=%v", got)
	}
	mu.Lock()
	require.Equal(t, []string{"m1", "m2", "m3", "m4", "m5"}, got)
	mu.Unlock()
}

func TestFakeOrderedReliableDifferentKeysParallel(t *testing.T) {
	provider := mq.NewFakeOrderedReliableProvider()
	started := make(chan string, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	_, err := provider.SubscribeReliable(context.Background(), "fills", mq.ReliableSubscribeOptions{Group: "g"}, func(msg *mq.Message) error {
		started <- string(msg.Data)
		<-release
		wg.Done()
		return nil
	})
	require.NoError(t, err)

	require.NoError(t, provider.Publish(context.Background(), "fills", []byte("a1"), &mq.PublishOptions{OrderingKey: "a"}))
	require.NoError(t, provider.Publish(context.Background(), "fills", []byte("b1"), &mq.PublishOptions{OrderingKey: "b"}))

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case body := <-started:
			got[body] = true
		case <-time.After(time.Second):
			t.Fatal("different keys should start in parallel")
		}
	}
	require.True(t, got["a1"] && got["b1"])
	close(release)
	wg.Wait()
}

func TestManagerRequireOrderedReliable(t *testing.T) {
	mgr := mq.NewManager()
	require.ErrorIs(t, mgr.RequireOrderedReliable(), mq.ErrOrderedReliableUnsupported)

	basic := &basicProvider{}
	mgr.Register(basic)
	require.NoError(t, mgr.SetCurrent("basic"))
	require.ErrorIs(t, mgr.RequireOrderedReliable(), mq.ErrOrderedReliableUnsupported)

	mgr2 := mq.NewManager()
	plain := mq.NewFakeOrderedReliableProvider()
	mgr2.Register(plain)
	require.NoError(t, mgr2.SetCurrent(plain.Name()))
	require.NoError(t, mgr2.RequireOrderedReliable())
}

type basicProvider struct{}

func (*basicProvider) Name() string                  { return "basic" }
func (*basicProvider) Connect(context.Context) error { return nil }
func (*basicProvider) Close() error                  { return nil }
func (*basicProvider) Health(context.Context) error  { return nil }
func (*basicProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	return nil
}
func (*basicProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
