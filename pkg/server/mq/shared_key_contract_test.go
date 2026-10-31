package mq_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

func TestSharedKeyRequirementRejectsOnlyLocallyKeyedProvider(t *testing.T) {
	m := mq.NewManager()
	p := &keyedReliableMockProvider{reliableMockProvider: reliableMockProvider{mockProvider: mockProvider{name: "local", healthy: true}}}
	m.Register(p)
	require.NoError(t, m.SetCurrent("local"))
	defer m.Close()
	var opts mq.ReliableSubscribeOptions
	require.NoError(t, json.Unmarshal([]byte(`{"Group":"users","SharedKeys":true}`), &opts))
	_, err := m.SubscribeReliable(context.Background(), "fills", opts, func(*mq.Message) error { return nil })
	require.ErrorContains(t, err, "shared key")
	require.Nil(t, p.handler, "不具备跨实例能力时不得调用底层订阅")
}

func TestSharedKeyNATSDoesNotAdvertiseOrSilentlyIgnoreRequirement(t *testing.T) {
	p := &mq.NATSJetStreamProvider{}
	var value any = p
	_, ordered := value.(mq.OrderedReliableMQProvider)
	_, keyed := value.(mq.KeyedReliableMQProvider)
	_, shared := value.(mq.SharedKeyReliableMQProvider)
	require.False(t, ordered)
	require.False(t, keyed)
	require.False(t, shared)
	_, err := p.SubscribeReliable(context.Background(), "fills", mq.ReliableSubscribeOptions{Group: "users", SharedKeys: true}, func(*mq.Message) error { return nil })
	require.ErrorIs(t, err, mq.ErrSharedKeyReliableUnsupported)
}

type sharedContractProvider struct{ keyedReliableMockProvider }

func (*sharedContractProvider) OrderedReliableInfo() mq.OrderedReliableCapability {
	return mq.DefaultOrderedReliableCapability()
}
func (*sharedContractProvider) SharedKeyReliableInfo() mq.SharedKeyReliableCapability {
	return mq.DefaultSharedKeyReliableCapability()
}
func TestSharedKeyContractCannotSilentlySwitchProvider(t *testing.T) {
	m := mq.NewManager()
	p := &sharedContractProvider{keyedReliableMockProvider{reliableMockProvider{mockProvider: mockProvider{name: "shared", healthy: true}}}}
	m.Register(p)
	require.NoError(t, m.SetCurrent("shared"))
	defer m.Close()
	require.NoError(t, m.RequireSharedKeyReliable())
	target := &sharedContractProvider{keyedReliableMockProvider{reliableMockProvider{mockProvider: mockProvider{name: "replacement", healthy: true}}}}
	m.Register(target)
	require.Error(t, m.SetCurrent("replacement"), "未验证的 live provider 切换不能绕过已声明的共享合同")
	require.Error(t, m.BeginSwitch(context.Background(), target, false))
	require.Nil(t, m.GetSwitcher())
}
