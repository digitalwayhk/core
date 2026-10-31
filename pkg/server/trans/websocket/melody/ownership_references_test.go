// 本文件验证从现有会话采样多客户端独立引用，以及订阅/退订完整集合收敛。
package melody

import (
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/olahol/melody"
	"github.com/stretchr/testify/require"
	"testing"
)

type ownedReferenceTestRouter struct {
	subscriptionLifecycleRouter
	Key string
}

func (r *ownedReferenceTestRouter) WebSocketOwnedReference() (string, string, string) {
	return "stats", r.Key, "kline"
}

func TestGatewayOwnershipSnapshotUsesExistingSessionReferences(t *testing.T) {
	mm := &MelodyManager{subscriptions: map[*melody.Session]*SessionSubscriptions{}}
	a, b := &melody.Session{}, &melody.Session{}
	first := NewSessionSubscriptions(mm, nil, nil)
	second := NewSessionSubscriptions(mm, nil, nil)
	first.subscriptions = map[string]map[uint64]types.IRouter{"/one": {1: &ownedReferenceTestRouter{Key: "A"}}, "/two": {2: &ownedReferenceTestRouter{Key: "A"}}}
	second.subscriptions = map[string]map[uint64]types.IRouter{"/one": {1: &ownedReferenceTestRouter{Key: "A"}}}
	mm.subscriptions[a] = first
	mm.subscriptions[b] = second
	refs, revision, err := mm.captureOwnedReferences()
	require.NoError(t, err)
	require.Positive(t, revision)
	require.Len(t, refs, 2, "同会话重复语义引用折叠，不同会话分别保留")
	first.mu.Lock()
	delete(first.subscriptions, "/one")
	first.mu.Unlock()
	refs, _, err = mm.captureOwnedReferences()
	require.NoError(t, err)
	require.Len(t, refs, 2, "同会话另一路由仍有引用")
	first.mu.Lock()
	delete(first.subscriptions, "/two")
	first.mu.Unlock()
	refs, _, err = mm.captureOwnedReferences()
	require.NoError(t, err)
	require.Len(t, refs, 1, "单个会话退订不能去掉其他会话引用")
}
