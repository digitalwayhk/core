package mq_test

import (
	"context"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

type reliableMockProvider struct {
	mockProvider
	options mq.ReliableSubscribeOptions
	handler func(*mq.Message) error
}

type keyedReliableMockProvider struct{ reliableMockProvider }

func (*keyedReliableMockProvider) SupportsKeyedReliableConcurrency() bool { return true }

func (p *reliableMockProvider) SubscribeReliable(
	_ context.Context,
	_ string,
	options mq.ReliableSubscribeOptions,
	handler func(*mq.Message) error,
) (func(), error) {
	p.options = options
	p.handler = handler
	return func() {}, nil
}

func TestMQManagerSubscribeReliableRequiresProviderCapability(t *testing.T) {
	manager := mq.NewManager()
	provider := &mockProvider{name: "legacy", healthy: true}
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent("legacy"))

	_, err := manager.SubscribeReliable(context.Background(), "orders", mq.ReliableSubscribeOptions{Group: "users"}, func(*mq.Message) error { return nil })
	require.ErrorIs(t, err, mq.ErrReliableSubscribeUnsupported)
}

func TestMQManagerSubscribeReliableDelegatesOptions(t *testing.T) {
	manager := mq.NewManager()
	provider := &reliableMockProvider{mockProvider: mockProvider{name: "reliable", healthy: true}}
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent("reliable"))
	options := mq.ReliableSubscribeOptions{Group: "user-service", Consumer: "user-1"}

	cancel, err := manager.SubscribeReliable(context.Background(), "orders", options, func(*mq.Message) error { return nil })
	require.NoError(t, err)
	defer cancel()
	require.Equal(t, options, provider.options)
}

func TestMQManagerSubscribeReliableRejectsUnsupportedKeyConcurrency(t *testing.T) {
	manager := mq.NewManager()
	provider := &reliableMockProvider{mockProvider: mockProvider{name: "reliable", healthy: true}}
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent("reliable"))

	_, err := manager.SubscribeReliable(context.Background(), "fills", mq.ReliableSubscribeOptions{
		Group: "positions", KeyConcurrency: 2,
	}, func(*mq.Message) error { return nil })
	require.ErrorIs(t, err, mq.ErrKeyedReliableSubscribeUnsupported)
}

func TestMQManagerSubscribeReliableDelegatesSupportedKeyConcurrency(t *testing.T) {
	manager := mq.NewManager()
	provider := &keyedReliableMockProvider{reliableMockProvider: reliableMockProvider{
		mockProvider: mockProvider{name: "keyed-reliable", healthy: true},
	}}
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent("keyed-reliable"))
	options := mq.ReliableSubscribeOptions{Group: "positions", KeyConcurrency: 4}

	cancel, err := manager.SubscribeReliable(context.Background(), "fills", options, func(*mq.Message) error { return nil })
	require.NoError(t, err)
	defer cancel()
	require.Equal(t, options, provider.options)
}
