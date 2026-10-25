// 本文件验证本地发布和可靠Broker入站的隔离、默认兼容、聚合确认及取消边界。
package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

type brokerOnlyProvider struct {
	reliableBridgeProvider
	mu            sync.Mutex
	published     [][]byte
	subscriptions int
	groups        map[string]func(*mq.Message) error
	canceled      int
	publishErr    error
}

func (p *brokerOnlyProvider) Publish(_ context.Context, _ string, data []byte, _ *mq.PublishOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.publishErr != nil {
		return p.publishErr
	}
	p.published = append(p.published, append([]byte(nil), data...))
	return nil
}
func (p *brokerOnlyProvider) SubscribeReliable(_ context.Context, _ string, opts mq.ReliableSubscribeOptions, h func(*mq.Message) error) (func(), error) {
	p.mu.Lock()
	p.subscriptions++
	p.handler = h
	if p.groups == nil {
		p.groups = map[string]func(*mq.Message) error{}
	}
	p.groups[opts.Group] = h
	p.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { p.mu.Lock(); p.canceled++; p.mu.Unlock() }) }, nil
}
func newBrokerOnlyBridge(t *testing.T) (*event.ServiceEventBridge, *brokerOnlyProvider) {
	t.Helper()
	p := &brokerOnlyProvider{}
	m := mq.NewManager()
	m.Register(p)
	require.NoError(t, m.SetCurrent(p.Name()))
	s := event.NewStream()
	b := event.NewServiceEventBridge(s, event.ServiceEventBridgeOptions{SubscriberID: "projection"})
	b.SetExternalPublisher(event.NewMQBridge(s, m))
	t.Cleanup(func() { require.NoError(t, b.Close(context.Background())); require.NoError(t, m.Close()) })
	return b, p
}
func brokerSubscription(t *testing.T, subject, kind string, broker bool, h func(context.Context, *event.Envelope) error) event.Subscription {
	t.Helper()
	sub := event.Subscription{}
	// 在无此字段的发布基线运行同一夹具，得到行为RED而不是编译失败。
	raw, err := json.Marshal(map[string]any{"Subject": subject, "EventType": kind, "Reliable": true, "BrokerOnly": broker})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &sub))
	sub.Handler = h
	return sub
}
func brokerEnvelope(subject, kind string) *event.Envelope {
	e := event.NewEnvelope("projection", kind, nil)
	e.Subject = subject
	e.ShardKey = "key-a"
	return e
}
func dispatchBroker(t *testing.T, p *brokerOnlyProvider, e *event.Envelope) error {
	t.Helper()
	raw, err := json.Marshal(e)
	require.NoError(t, err)
	return p.handler(&mq.Message{Subject: e.Subject, Data: raw})
}

// TestDefaultControlHandlerBlocksExternalPublish 固定旧默认同步控制处理先于外发的真实调用链。
func TestDefaultControlHandlerBlocksExternalPublish(t *testing.T) {
	b, p := newBrokerOnlyBridge(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	stop, err := b.SubscribeEvent(brokerSubscription(t, "projection.events", "changed", false, func(context.Context, *event.Envelope) error { close(entered); <-release; return nil }))
	require.NoError(t, err)
	defer stop()
	done := make(chan error, 1)
	go func() {
		done <- b.Publish(context.Background(), event.PublishRequest{Class: event.ControlDelivery, External: true, Subject: "projection.events", Envelope: brokerEnvelope("projection.events", "changed")})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("本地handler未进入")
	}
	p.mu.Lock()
	require.Empty(t, p.published)
	p.mu.Unlock()
	select {
	case <-done:
		t.Fatal("本地handler完成前不得外发/返回")
	default:
	}
	once.Do(func() { close(release) })
	require.NoError(t, <-done)
	p.mu.Lock()
	require.Len(t, p.published, 1)
	p.mu.Unlock()
}

// TestBrokerOnlySeparatesPublishAndReliableIngress 保留原Source且本地发布不调用，真实MQBridge入站调用。
func TestBrokerOnlySeparatesPublishAndReliableIngress(t *testing.T) {
	b, p := newBrokerOnlyBridge(t)
	var calls atomic.Int32
	stop, err := b.SubscribeEvent(brokerSubscription(t, "projection.events", "changed", true, func(context.Context, *event.Envelope) error { calls.Add(1); return nil }))
	require.NoError(t, err)
	defer stop()
	env := brokerEnvelope("projection.events", "changed")
	require.NoError(t, b.Publish(context.Background(), event.PublishRequest{Class: event.ControlDelivery, External: true, Subject: env.Subject, Envelope: env}))
	require.Zero(t, calls.Load(), "显式仅Broker订阅不得在发布阶段执行")
	require.NoError(t, dispatchBroker(t, p, env))
	require.EqualValues(t, 1, calls.Load(), "同服务Source也必须消费")
}

// TestBrokerOnlyMixedHandlersAndCancellation 验证通配、指定类型、Subject隔离和单独取消不移除其他闭包。
func TestBrokerOnlyMixedHandlersAndCancellation(t *testing.T) {
	b, p := newBrokerOnlyBridge(t)
	var legacy, wild, exact, other atomic.Int32
	stops := []func(){}
	add := func(subject, kind string, only bool, c *atomic.Int32) {
		stop, err := b.SubscribeEvent(brokerSubscription(t, subject, kind, only, func(context.Context, *event.Envelope) error { c.Add(1); return nil }))
		require.NoError(t, err)
		stops = append(stops, stop)
	}
	add("projection.events", "changed", false, &legacy)
	add("projection.events", "", true, &wild)
	add("projection.events", "changed", true, &exact)
	// 不同Subject不建立第二个fake回调；直接过滤不匹配的Envelope。
	e := brokerEnvelope("projection.events", "changed")
	require.NoError(t, b.Publish(context.Background(), event.PublishRequest{Class: event.ControlDelivery, Envelope: e}))
	require.EqualValues(t, 1, legacy.Load())
	require.Zero(t, wild.Load())
	require.Zero(t, exact.Load())
	require.NoError(t, dispatchBroker(t, p, e))
	require.EqualValues(t, 2, legacy.Load())
	require.EqualValues(t, 1, wild.Load())
	require.EqualValues(t, 1, exact.Load())
	require.NoError(t, dispatchBroker(t, p, brokerEnvelope("other.events", "changed")))
	require.Zero(t, other.Load())
	require.EqualValues(t, 1, wild.Load())
	require.NoError(t, dispatchBroker(t, p, brokerEnvelope("projection.events", "other-type")))
	require.EqualValues(t, 2, wild.Load())
	require.EqualValues(t, 1, exact.Load())
	stops[2]()
	stops[2]()
	require.NoError(t, dispatchBroker(t, p, e))
	require.EqualValues(t, 3, legacy.Load())
	require.EqualValues(t, 3, wild.Load())
	require.EqualValues(t, 1, exact.Load())
	for _, stop := range stops {
		stop()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.subscriptions)
	require.Equal(t, 1, p.canceled)
}

// TestBrokerOnlyAggregatesFailures 不吞错误，全部匹配handler完成后才可让可靠provider确认。
func TestBrokerOnlyAggregatesFailures(t *testing.T) {
	b, p := newBrokerOnlyBridge(t)
	want := errors.New("durable transaction failed")
	var second atomic.Int32
	stop, err := b.SubscribeEvent(brokerSubscription(t, "projection.events", "changed", true, func(context.Context, *event.Envelope) error { return want }))
	require.NoError(t, err)
	defer stop()
	stop2, err := b.SubscribeEvent(brokerSubscription(t, "projection.events", "", true, func(context.Context, *event.Envelope) error { second.Add(1); return nil }))
	require.NoError(t, err)
	defer stop2()
	require.ErrorIs(t, dispatchBroker(t, p, brokerEnvelope("projection.events", "changed")), want)
	require.EqualValues(t, 1, second.Load())
}

// TestBrokerOnlyInvalidOrUnavailableFailsClosed 不接受无可靠handler/空Subject或无真正外部接线。
func TestBrokerOnlyInvalidOrUnavailableFailsClosed(t *testing.T) {
	for _, tc := range []string{"observer", "empty-subject", "whitespace-subject", "no-adapter", "legacy-adapter", "manager-off"} {
		t.Run(tc, func(t *testing.T) {
			b := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "projection"})
			defer b.Close(context.Background())
			sub := brokerSubscription(t, "events", "changed", true, func(context.Context, *event.Envelope) error { return nil })
			switch tc {
			case "observer":
				sub.Reliable = false
			case "empty-subject":
				sub.Subject = ""
			case "whitespace-subject":
				sub.Subject = "  "
			case "legacy-adapter":
				b.SetExternalPublisher(&fakeExternalEventAdapter{})
			case "manager-off":
				b.SetExternalPublisher(event.NewMQBridge(b.Stream(), mq.NewManager()))
			}
			if tc == "observer" || tc == "empty-subject" || tc == "whitespace-subject" {
				m := mq.NewManager()
				m.Register(&brokerOnlyProvider{})
				require.NoError(t, m.SetCurrent("reliable-bridge"))
				b.SetExternalPublisher(event.NewMQBridge(b.Stream(), m))
				defer m.Close()
			}
			stop, err := b.SubscribeEvent(sub)
			if stop != nil {
				stop()
			}
			require.Error(t, err, "不得注册成无消费者的成功订阅")
		})
	}
}

// TestBrokerOnlyCloseWaitsForActualHandlerCloseTimeout 不合作handler仍在执行时Close不能伪装已退出或提前成功ACK。
func TestBrokerOnlyCloseWaitsForActualHandlerCloseTimeout(t *testing.T) {
	b, p := newBrokerOnlyBridge(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var canceled atomic.Bool
	stop, err := b.SubscribeEvent(brokerSubscription(t, "events", "changed", true, func(ctx context.Context, _ *event.Envelope) error {
		close(entered)
		<-ctx.Done()
		canceled.Store(true)
		<-release
		return nil
	}))
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- dispatchBroker(t, p, brokerEnvelope("events", "changed")) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("未进入handler")
	}
	stop()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	require.Error(t, b.Close(ctx))
	require.True(t, canceled.Load())
	select {
	case <-done:
		t.Fatal("实际handler仍未退出")
	default:
	}
	once.Do(func() { close(release) })
	require.Error(t, <-done, "取消后迟到成功不能ACK")
	require.NoError(t, b.Close(context.Background()))
}

// TestBrokerOnlyConcurrentRegistrationReusesOneGroup 并发注册复用一个实际Broker订阅，单独取消只移除自己的handler。
func TestBrokerOnlyConcurrentRegistrationReusesOneGroup(t *testing.T) {
	b, p := newBrokerOnlyBridge(t)
	const n = 32
	stops := make([]func(), n)
	counts := make([]atomic.Int32, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := range n {
		sub := brokerSubscription(t, "events", "changed", i%2 == 0, func(context.Context, *event.Envelope) error { counts[i].Add(1); return nil })
		wg.Add(1)
		go func() { defer wg.Done(); stop, err := b.SubscribeEvent(sub); stops[i] = stop; errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for i := 0; i < n; i += 2 {
		stops[i]()
	}
	require.NoError(t, dispatchBroker(t, p, brokerEnvelope("events", "changed")))
	for i := range n {
		if i%2 == 0 {
			require.Zero(t, counts[i].Load())
		} else {
			require.EqualValues(t, 1, counts[i].Load())
		}
		stops[i]()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, 1, p.subscriptions)
	require.Equal(t, 1, p.canceled)
}

// TestBrokerOnlyReliableWaitsForAllMatchingHandlers 不以第一个成功掩盖后一个仍在执行的handler。
func TestBrokerOnlyReliableWaitsForAllMatchingHandlers(t *testing.T) {
	b, p := newBrokerOnlyBridge(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	stop, err := b.SubscribeEvent(brokerSubscription(t, "events", "changed", false, func(context.Context, *event.Envelope) error { return nil }))
	require.NoError(t, err)
	defer stop()
	stop2, err := b.SubscribeEvent(brokerSubscription(t, "events", "", true, func(context.Context, *event.Envelope) error { close(entered); <-release; return nil }))
	require.NoError(t, err)
	defer stop2()
	done := make(chan error, 1)
	go func() { done <- dispatchBroker(t, p, brokerEnvelope("events", "changed")) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("通配handler未执行")
	}
	select {
	case <-done:
		t.Fatal("全部handler完成前不得向Provider返回成功")
	default:
	}
	once.Do(func() { close(release) })
	require.NoError(t, <-done)
}

// TestBrokerOnlyBroadcastGroupIsolation 同主题的逻辑组和每副本Broadcast各走自己的可靠入站，不重复执行新模式handler。
func TestBrokerOnlyBroadcastGroupIsolation(t *testing.T) {
	p := &brokerOnlyProvider{}
	m := mq.NewManager()
	m.Register(p)
	require.NoError(t, m.SetCurrent(p.Name()))
	stream := event.NewStream()
	b := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{SubscriberID: "projection", InstanceSubscriberID: "projection@node"})
	b.SetExternalPublisher(event.NewMQBridge(stream, m))
	defer m.Close()
	defer b.Close(context.Background())
	var normal, broadcast atomic.Int32
	sub := brokerSubscription(t, "events", "changed", true, func(context.Context, *event.Envelope) error { normal.Add(1); return nil })
	stop, err := b.SubscribeEvent(sub)
	require.NoError(t, err)
	defer stop()
	sub.Handler = func(context.Context, *event.Envelope) error { broadcast.Add(1); return nil }
	sub.Broadcast = true
	stop2, err := b.SubscribeEvent(sub)
	require.NoError(t, err)
	defer stop2()
	raw, err := json.Marshal(brokerEnvelope("events", "changed"))
	require.NoError(t, err)
	require.NoError(t, p.groups["projection"](&mq.Message{Subject: "events", Data: raw}))
	require.EqualValues(t, 1, normal.Load())
	require.Zero(t, broadcast.Load())
	require.NoError(t, p.groups["projection@node"](&mq.Message{Subject: "events", Data: raw}))
	require.EqualValues(t, 1, normal.Load())
	require.EqualValues(t, 1, broadcast.Load())
}

// TestBrokerOnlyRejectsDetachedMQIngress 适配器若接在另一个Stream上，不能假报已注册而实际没有消费者。
func TestBrokerOnlyRejectsDetachedMQIngress(t *testing.T) {
	m := mq.NewManager()
	m.Register(&brokerOnlyProvider{})
	require.NoError(t, m.SetCurrent("reliable-bridge"))
	defer m.Close()
	b := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{SubscriberID: "projection"})
	defer b.Close(context.Background())
	b.SetExternalPublisher(event.NewMQBridge(event.NewStream(), m))
	stop, err := b.SubscribeEvent(brokerSubscription(t, "events", "changed", true, func(context.Context, *event.Envelope) error { return nil }))
	if stop != nil {
		stop()
	}
	require.Error(t, err, "可靠入站必须接回本bridge注册handler的Stream")
}
