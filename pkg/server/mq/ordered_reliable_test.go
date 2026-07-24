package mq_test

import (
	"context"
	"errors"
	"fmt"
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

func TestFakeOrderedReliableOneHundredStrictOrder(t *testing.T) {
	provider := mq.NewFakeOrderedReliableProvider()
	const n = 100
	got := make([]int, 0, n)
	done := make(chan struct{})
	_, err := provider.SubscribeReliable(context.Background(), "fills", mq.ReliableSubscribeOptions{Group: "g"}, func(msg *mq.Message) error {
		var v int
		_, _ = fmt.Sscanf(string(msg.Data), "%d", &v)
		got = append(got, v)
		if len(got) == n {
			close(done)
		}
		return nil
	})
	require.NoError(t, err)
	for i := 1; i <= n; i++ {
		body := fmt.Sprintf("%d", i)
		require.NoError(t, provider.Publish(context.Background(), "fills", []byte(body), &mq.PublishOptions{
			OrderingKey: "k", IdempotencyKey: body,
		}))
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout got=%d", len(got))
	}
	for i := 0; i < n; i++ {
		require.Equal(t, i+1, got[i])
	}
}

func TestFakeOrderedReliableRedeliveryKeepsIdentity(t *testing.T) {
	provider := mq.NewFakeOrderedReliableProvider()
	var (
		attempts atomic.Int32
		seen     *mq.Message
		done     = make(chan struct{})
	)
	_, err := provider.SubscribeReliable(context.Background(), "fills", mq.ReliableSubscribeOptions{Group: "g"}, func(msg *mq.Message) error {
		if attempts.Add(1) == 1 {
			seen = &mq.Message{ID: msg.ID, Subject: msg.Subject, Data: append([]byte(nil), msg.Data...)}
			return errors.New("retry")
		}
		require.Equal(t, seen.ID, msg.ID)
		require.Equal(t, seen.Subject, msg.Subject)
		require.Equal(t, seen.Data, msg.Data)
		close(done)
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, provider.Publish(context.Background(), "fills", []byte(`{"x":1}`), &mq.PublishOptions{
		OrderingKey: "market-a", IdempotencyKey: "evt-1",
	}))
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	require.Equal(t, "evt-1", seen.ID)
}

// lyingOrderedProvider 自报合法 capability，但不做失败阻断（同 key 可越序推进）。
type lyingOrderedProvider struct {
	mu   sync.Mutex
	subs []func(*mq.Message) error
}

func (*lyingOrderedProvider) Name() string                  { return "lying-ordered" }
func (*lyingOrderedProvider) Connect(context.Context) error { return nil }
func (*lyingOrderedProvider) Close() error                  { return nil }
func (*lyingOrderedProvider) Health(context.Context) error  { return nil }
func (*lyingOrderedProvider) OrderedReliableInfo() mq.OrderedReliableCapability {
	return mq.DefaultOrderedReliableCapability()
}
func (*lyingOrderedProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
func (p *lyingOrderedProvider) Publish(_ context.Context, subject string, data []byte, _ *mq.PublishOptions) error {
	p.mu.Lock()
	handlers := append([]func(*mq.Message) error(nil), p.subs...)
	p.mu.Unlock()
	msg := &mq.Message{ID: string(data), Subject: subject, Data: data, Ack: func() error { return nil }}
	for _, h := range handlers {
		_ = h(msg) // 故意忽略错误并继续——违反失败阻断
	}
	return nil
}
func (p *lyingOrderedProvider) SubscribeReliable(_ context.Context, _ string, _ mq.ReliableSubscribeOptions, handler func(*mq.Message) error) (func(), error) {
	p.mu.Lock()
	p.subs = append(p.subs, handler)
	p.mu.Unlock()
	return func() {}, nil
}

func TestConformanceRejectsLyingOrderedProvider(t *testing.T) {
	// §7.10：capability 自报合法但实际不阻断 → conformance 必须拒绝。
	lying := &lyingOrderedProvider{}
	mgr := mq.NewManager()
	mgr.Register(lying)
	require.NoError(t, mgr.SetCurrent(lying.Name()))
	// 启动门禁仅看 Info，撒谎者可通过（已知限制）；conformance 行为套件必须抓出。
	require.NoError(t, mgr.RequireOrderedReliable())
	require.Error(t, mq.VerifyOrderedReliableFailureBarrier(lying))
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
