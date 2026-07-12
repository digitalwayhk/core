package mq_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

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
