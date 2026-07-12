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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a test double for mq.MQProvider.
type mockProvider struct {
	name       string
	healthy    bool
	published  [][]byte
	closed     bool
	closeCalls atomic.Int32
	closeErr   error
}

type blockingProvider struct {
	name         string
	operation    string
	entered      chan struct{}
	release      chan struct{}
	closeStarted chan struct{}
	closeOnce    sync.Once
}

func (p *blockingProvider) Name() string                { return p.name }
func (*blockingProvider) Connect(context.Context) error { return nil }
func (p *blockingProvider) Close() error {
	p.closeOnce.Do(func() { close(p.closeStarted) })
	return nil
}
func (p *blockingProvider) block(operation string) {
	if p.operation != operation {
		return
	}
	close(p.entered)
	<-p.release
}
func (p *blockingProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	p.block("publish")
	return nil
}
func (p *blockingProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	p.block("subscribe")
	return func() {}, nil
}
func (p *blockingProvider) Health(context.Context) error {
	p.block("health")
	return nil
}

func TestMQManagerClose_WaitsForInFlightOperations(t *testing.T) {
	for _, operation := range []string{"publish", "subscribe", "health"} {
		t.Run(operation, func(t *testing.T) {
			provider := &blockingProvider{
				name:         operation,
				operation:    operation,
				entered:      make(chan struct{}),
				release:      make(chan struct{}),
				closeStarted: make(chan struct{}),
			}
			mgr := mq.NewManager()
			mgr.Register(provider)
			require.NoError(t, mgr.SetCurrent(provider.Name()))

			operationDone := make(chan error, 1)
			go func() {
				switch operation {
				case "publish":
					operationDone <- mgr.Publish(context.Background(), "subject", nil, nil)
				case "subscribe":
					_, err := mgr.Subscribe(context.Background(), "subject", func(*mq.Message) {})
					operationDone <- err
				case "health":
					operationDone <- mgr.Health(context.Background())
				}
			}()
			<-provider.entered

			closeDone := make(chan error, 1)
			go func() { closeDone <- mgr.Close() }()
			select {
			case <-provider.closeStarted:
				t.Fatal("provider.Close 与 manager 发起的在途操作交叉")
			case <-time.After(20 * time.Millisecond):
			}

			close(provider.release)
			require.NoError(t, <-operationDone)
			require.NoError(t, <-closeDone)
			assert.ErrorIs(t, mgr.Publish(context.Background(), "subject", nil, nil), mq.ErrNotConnected)
		})
	}
}

func TestMQManagerCurrent_ReturnsSnapshotOutsideLifecycleGate(t *testing.T) {
	provider := &blockingProvider{
		name:         "snapshot",
		operation:    "publish",
		entered:      make(chan struct{}),
		release:      make(chan struct{}),
		closeStarted: make(chan struct{}),
	}
	mgr := mq.NewManager()
	mgr.Register(provider)
	require.NoError(t, mgr.SetCurrent(provider.Name()))

	snapshot := mgr.Current()
	require.Same(t, provider, snapshot)
	snapshotDone := make(chan error, 1)
	go func() {
		snapshotDone <- snapshot.Publish(context.Background(), "subject", nil, nil)
	}()
	<-provider.entered

	closeDone := make(chan error, 1)
	go func() { closeDone <- mgr.Close() }()
	select {
	case <-provider.closeStarted:
		// Current 返回的是快照，直接调用不属于 Manager 的生命周期门禁。
	case <-time.After(time.Second):
		t.Fatal("Manager.Close 被 Current 快照的直接调用阻塞")
	}
	require.NoError(t, <-closeDone)
	close(provider.release)
	require.NoError(t, <-snapshotDone)
}

type asyncHandlerProvider struct {
	handlerEntered chan struct{}
	releaseHandler chan struct{}
	handlerDone    chan struct{}
	closeStarted   chan struct{}
}

func (*asyncHandlerProvider) Name() string                  { return "async-handler" }
func (*asyncHandlerProvider) Connect(context.Context) error { return nil }
func (p *asyncHandlerProvider) Close() error {
	close(p.closeStarted)
	return nil
}
func (*asyncHandlerProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	return nil
}
func (p *asyncHandlerProvider) Subscribe(_ context.Context, _ string, handler func(*mq.Message)) (func(), error) {
	go func() {
		defer close(p.handlerDone)
		close(p.handlerEntered)
		handler(&mq.Message{})
	}()
	return func() {}, nil
}
func (*asyncHandlerProvider) Health(context.Context) error { return nil }

func TestMQManagerSubscribe_UserHandlerDoesNotHoldLifecycleGate(t *testing.T) {
	provider := &asyncHandlerProvider{
		handlerEntered: make(chan struct{}),
		releaseHandler: make(chan struct{}),
		handlerDone:    make(chan struct{}),
		closeStarted:   make(chan struct{}),
	}
	mgr := mq.NewManager()
	mgr.Register(provider)
	require.NoError(t, mgr.SetCurrent(provider.Name()))

	_, err := mgr.Subscribe(context.Background(), "subject", func(*mq.Message) {
		<-provider.releaseHandler
	})
	require.NoError(t, err)
	<-provider.handlerEntered

	closeDone := make(chan error, 1)
	go func() { closeDone <- mgr.Close() }()
	select {
	case <-provider.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("用户 handler 执行期间阻塞了 Manager.Close")
	}
	require.NoError(t, <-closeDone)
	close(provider.releaseHandler)
	select {
	case <-provider.handlerDone:
	case <-time.After(time.Second):
		t.Fatal("用户 handler 未退出")
	}
}

type orderedCloseProvider struct {
	name     string
	closeErr error
	order    *[]string
	mu       *sync.Mutex
	calls    atomic.Int32
}

func (p *orderedCloseProvider) Name() string                { return p.name }
func (*orderedCloseProvider) Connect(context.Context) error { return nil }
func (p *orderedCloseProvider) Close() error {
	p.calls.Add(1)
	p.mu.Lock()
	*p.order = append(*p.order, p.closeErr.Error())
	p.mu.Unlock()
	return p.closeErr
}
func (*orderedCloseProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	return nil
}
func (*orderedCloseProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
func (*orderedCloseProvider) Health(context.Context) error { return nil }

func TestMQManagerClose_IsDeterministicAndDeduplicatesByInstance(t *testing.T) {
	var mu sync.Mutex
	var order []string
	providerA := &orderedCloseProvider{name: "key-b", closeErr: errors.New("error-a"), order: &order, mu: &mu}
	providerB := &orderedCloseProvider{name: "key-a", closeErr: errors.New("error-b"), order: &order, mu: &mu}
	providerC := &orderedCloseProvider{name: "key-c", closeErr: errors.New("error-c"), order: &order, mu: &mu}
	mgr := mq.NewManager()
	mgr.Register(providerA)
	require.NoError(t, mgr.SetCurrent("key-b"))
	mgr.Register(providerB)
	mgr.Register(providerC)
	providerA.name = "key-d"
	mgr.Register(providerA)
	providerA.name = "shared"
	providerB.name = "shared"
	providerC.name = "shared"

	err := mgr.Close()
	require.EqualError(t, err, fmt.Sprintf("mq: close provider %q: error-b\nmq: close provider %q: error-a\nmq: close provider %q: error-c", "shared", "shared", "shared"))
	assert.Equal(t, []string{"error-b", "error-a", "error-c"}, order)
	assert.Equal(t, int32(1), providerA.calls.Load(), "同一指针多 key 注册只关闭一次")
	assert.Equal(t, int32(1), providerB.calls.Load(), "同名不同实例必须关闭")
	assert.Equal(t, int32(1), providerC.calls.Load(), "同名不同实例必须关闭")
}

func TestMQManagerClose_ConcurrentOperationsStress(t *testing.T) {
	for iteration := 0; iteration < 25; iteration++ {
		mgr := mq.NewManager()
		provider := &blockingProvider{name: "stress", closeStarted: make(chan struct{})}
		mgr.Register(provider)
		require.NoError(t, mgr.SetCurrent(provider.Name()))

		start := make(chan struct{})
		errs := make(chan error, 24)
		var wg sync.WaitGroup
		for worker := 0; worker < 8; worker++ {
			wg.Add(3)
			go func() {
				defer wg.Done()
				<-start
				errs <- mgr.Health(context.Background())
			}()
			go func() {
				defer wg.Done()
				<-start
				errs <- mgr.Publish(context.Background(), "subject", nil, nil)
			}()
			go func() {
				defer wg.Done()
				<-start
				_, err := mgr.Subscribe(context.Background(), "subject", func(*mq.Message) {})
				errs <- err
			}()
		}
		close(start)
		require.NoError(t, mgr.Close())
		wg.Wait()
		close(errs)
		for err := range errs {
			assert.True(t, err == nil || errors.Is(err, mq.ErrNotConnected), "unexpected operation error: %v", err)
		}
	}
}

func (m *mockProvider) Name() string                    { return m.name }
func (m *mockProvider) Connect(_ context.Context) error { return nil }
func (m *mockProvider) Close() error {
	m.closed = true
	m.closeCalls.Add(1)
	return m.closeErr
}

func TestMQManagerClose_ClosesDistinctProvidersOnceAndDisconnects(t *testing.T) {
	mgr := mq.NewManager()
	firstErr := errors.New("close first")
	secondErr := errors.New("close second")
	first := &mockProvider{name: "first", healthy: true, closeErr: firstErr}
	second := &mockProvider{name: "second", healthy: true, closeErr: secondErr}

	mgr.Register(first)
	require.NoError(t, mgr.SetCurrent(first.Name()))
	// The same provider can appear under multiple registry keys if its name changes.
	first.name = "first-alias"
	mgr.Register(first)
	mgr.Register(second)

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- mgr.Close()
		}()
	}
	wg.Wait()
	close(errs)

	var joined error
	for err := range errs {
		if err != nil {
			joined = err
		}
	}
	require.ErrorIs(t, joined, firstErr)
	require.ErrorIs(t, joined, secondErr)
	assert.Equal(t, int32(1), first.closeCalls.Load())
	assert.Equal(t, int32(1), second.closeCalls.Load())
	assert.Nil(t, mgr.Current())
	assert.ErrorIs(t, mgr.Health(context.Background()), mq.ErrNotConnected)
	assert.ErrorIs(t, mgr.Publish(context.Background(), "subject", nil, nil), mq.ErrNotConnected)
	_, err := mgr.Subscribe(context.Background(), "subject", func(*mq.Message) {})
	assert.ErrorIs(t, err, mq.ErrNotConnected)
}

func TestMQManagerClose_ClosesCurrentAndReplacementWithSameName(t *testing.T) {
	mgr := mq.NewManager()
	oldProvider := &mockProvider{name: "shared", healthy: true}
	newProvider := &mockProvider{name: "shared", healthy: true}

	mgr.Register(oldProvider)
	require.NoError(t, mgr.SetCurrent("shared"))
	mgr.Register(newProvider)

	require.NoError(t, mgr.Close())
	assert.Equal(t, int32(1), oldProvider.closeCalls.Load())
	assert.Equal(t, int32(1), newProvider.closeCalls.Load())
}
func (m *mockProvider) Publish(_ context.Context, _ string, data []byte, _ *mq.PublishOptions) error {
	if !m.healthy {
		return errors.New("provider unhealthy")
	}
	m.published = append(m.published, data)
	return nil
}
func (m *mockProvider) Subscribe(_ context.Context, _ string, _ func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
func (m *mockProvider) Health(_ context.Context) error {
	if !m.healthy {
		return errors.New("provider unhealthy")
	}
	return nil
}

// --- MQManager tests ---

func TestMQManager_RegisterAndCurrent(t *testing.T) {
	mgr := mq.NewManager()
	p := &mockProvider{name: "redis-stream", healthy: true}
	mgr.Register(p)
	require.NoError(t, mgr.SetCurrent("redis-stream"))
	assert.Equal(t, "redis-stream", mgr.Current().Name())
}

func TestMQManager_SetCurrent_UnregisteredReturnsError(t *testing.T) {
	mgr := mq.NewManager()
	assert.Error(t, mgr.SetCurrent("unknown"))
}

func TestMQManager_Health_Healthy(t *testing.T) {
	mgr := mq.NewManager()
	p := &mockProvider{name: "redis-stream", healthy: true}
	mgr.Register(p)
	require.NoError(t, mgr.SetCurrent("redis-stream"))
	assert.NoError(t, mgr.Health(context.Background()))
}

func TestMQManager_Health_Unhealthy(t *testing.T) {
	mgr := mq.NewManager()
	p := &mockProvider{name: "redis-stream", healthy: false}
	mgr.Register(p)
	require.NoError(t, mgr.SetCurrent("redis-stream"))
	assert.Error(t, mgr.Health(context.Background()))
}

func TestMQManager_Health_NoCurrent(t *testing.T) {
	mgr := mq.NewManager()
	assert.ErrorIs(t, mgr.Health(context.Background()), mq.ErrNotConnected)
}

func TestMQManager_Publish_Delegates(t *testing.T) {
	mgr := mq.NewManager()
	p := &mockProvider{name: "redis-stream", healthy: true}
	mgr.Register(p)
	require.NoError(t, mgr.SetCurrent("redis-stream"))
	require.NoError(t, mgr.Publish(context.Background(), "test-subject", []byte("hello"), nil))
	assert.Equal(t, [][]byte{[]byte("hello")}, p.published)
}

// --- MQSwitcher tests ---

func TestMQSwitcher_DoubleWrite(t *testing.T) {
	mgr := mq.NewManager()
	oldP := &mockProvider{name: "redis-stream", healthy: true}
	mgr.Register(oldP)
	require.NoError(t, mgr.SetCurrent("redis-stream"))

	newP := &mockProvider{name: "nats-jetstream", healthy: true}
	sw := mq.NewSwitcher(mgr, true)

	require.NoError(t, sw.Begin(context.Background(), newP))
	assert.Equal(t, mq.SwitchStageDoubleWrite, sw.Stage())

	data := []byte("test-message")
	require.NoError(t, sw.DoubleWritePublish(context.Background(), "subj", data, nil))

	// Both providers should have received the message.
	assert.Len(t, oldP.published, 1)
	assert.Len(t, newP.published, 1)
}

func TestMQSwitcher_AdvanceToCatchUp(t *testing.T) {
	mgr := mq.NewManager()
	oldP := &mockProvider{name: "redis-stream", healthy: true}
	mgr.Register(oldP)
	require.NoError(t, mgr.SetCurrent("redis-stream"))

	newP := &mockProvider{name: "nats-jetstream", healthy: true}
	sw := mq.NewSwitcher(mgr, true)

	require.NoError(t, sw.Begin(context.Background(), newP))
	require.NoError(t, sw.AdvanceToCatchUp())
	assert.Equal(t, mq.SwitchStageCatchUp, sw.Stage())
}

func TestMQSwitcher_AdvanceToReadNew_SwitchesCurrent(t *testing.T) {
	mgr := mq.NewManager()
	oldP := &mockProvider{name: "redis-stream", healthy: true}
	mgr.Register(oldP)
	require.NoError(t, mgr.SetCurrent("redis-stream"))

	newP := &mockProvider{name: "nats-jetstream", healthy: true}
	sw := mq.NewSwitcher(mgr, true)

	require.NoError(t, sw.Begin(context.Background(), newP))
	require.NoError(t, sw.AdvanceToCatchUp())
	require.NoError(t, sw.AdvanceToReadNew())

	// Manager's current should now be the new provider.
	assert.Equal(t, "nats-jetstream", mgr.Current().Name())
	assert.Equal(t, mq.SwitchStageReadNew, sw.Stage())
}

func TestMQSwitcher_Rollback_RestoresOld(t *testing.T) {
	mgr := mq.NewManager()
	oldP := &mockProvider{name: "redis-stream", healthy: true}
	mgr.Register(oldP)
	require.NoError(t, mgr.SetCurrent("redis-stream"))

	newP := &mockProvider{name: "nats-jetstream", healthy: true}
	sw := mq.NewSwitcher(mgr, true)

	require.NoError(t, sw.Begin(context.Background(), newP))
	require.NoError(t, sw.Rollback())

	// Manager's current should be restored to the old provider.
	assert.Equal(t, "redis-stream", mgr.Current().Name())
	// New provider should have been closed.
	assert.True(t, newP.closed)
	// Stage should be idle again.
	assert.Equal(t, mq.SwitchStageIdle, sw.Stage())
}

func TestMQSwitcher_DoubleWriteFailure_AutoRollback(t *testing.T) {
	mgr := mq.NewManager()
	oldP := &mockProvider{name: "redis-stream", healthy: true}
	mgr.Register(oldP)
	require.NoError(t, mgr.SetCurrent("redis-stream"))

	newP := &mockProvider{name: "nats-jetstream", healthy: false} // will fail on publish
	sw := mq.NewSwitcher(mgr, true)

	require.NoError(t, sw.Begin(context.Background(), newP))
	err := sw.DoubleWritePublish(context.Background(), "subj", []byte("data"), nil)
	assert.Error(t, err)
	// Auto-rollback should have restored the old provider.
	assert.Equal(t, "redis-stream", mgr.Current().Name())
}
