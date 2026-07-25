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
