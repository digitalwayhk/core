// 本文件使用真实Redis验证持续租约、未知元数据及跨Gateway快照的安全边界。
package mq

import (
	"context"
	"errors"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func ownershipFixture(t *testing.T) (*RedisStreamProvider, ownership.Store, ownership.Manifest) {
	t.Helper()
	p := newPoolBudgetProvider(t, 4, 8)
	m := ownership.Manifest{Scope: "stats", Service: "quotes", Group: "quotes", Selector: "market-v1", Subjects: []string{"one", "two"}, Keys: []string{"A", "B"}}.Normalize()
	require.NoError(t, p.ProvisionOwnership(context.Background(), m, 0))
	s, err := p.OwnershipStore(context.Background())
	require.NoError(t, err)
	return p, s, m
}
func ownershipCandidate(m ownership.Manifest, key, instance string) ownership.Credential {
	return ownership.Credential{Scope: m.Scope, Key: key, Service: m.Service, InstanceID: instance, SessionID: instance + "-boot"}
}

func TestRedisOwnershipContinuousLeaseAndEpoch(t *testing.T) {
	_, s, m := ownershipFixture(t)
	ctx := context.Background()
	a, err := s.Acquire(ctx, ownershipCandidate(m, "A", "a"), m.Timing.Lease)
	require.NoError(t, err)
	require.Positive(t, a.Epoch)
	require.NoError(t, a.Validate())
	_, err = s.Resolve(ctx, m.Scope, "A")
	require.ErrorIs(t, err, ownership.ErrNotReady)
	_, err = s.Acquire(ctx, ownershipCandidate(m, "A", "b"), m.Timing.Lease)
	require.ErrorIs(t, err, ownership.ErrBusy)
	b, err := s.Acquire(ctx, ownershipCandidate(m, "B", "b"), m.Timing.Lease)
	require.NoError(t, err)
	require.NotEqual(t, a.Token, b.Token)
	require.NoError(t, s.Ready(ctx, a))
	found, err := s.Resolve(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.True(t, a.Same(found))
	require.NoError(t, s.Renew(ctx, a, m.Timing.Lease))
	require.NoError(t, s.Release(ctx, a))
	next, err := s.Acquire(ctx, ownershipCandidate(m, "A", "b"), m.Timing.Lease)
	require.NoError(t, err)
	require.Greater(t, next.Epoch, a.Epoch)
	require.ErrorIs(t, s.Ready(ctx, a), ownership.ErrFenced)
	require.ErrorIs(t, s.Release(ctx, a), ownership.ErrFenced)
	require.NoError(t, s.Validate(ctx, next, false))
}

func TestRedisOwnershipMissingMetadataCannotInventEpoch(t *testing.T) {
	p, s, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	c, err := s.Acquire(ctx, ownershipCandidate(m, "A", "a"), m.Timing.Lease)
	require.NoError(t, err)
	// 专属测试前缀故障注入：不删除任何业务Stream、group或PEL。
	keys, err := p.client.Keys(ctx, p.prefix+":ownership:*").Result()
	require.NoError(t, err)
	for _, key := range keys {
		if p.client.Type(ctx, key).Val() == "string" {
			require.NoError(t, p.client.Del(ctx, key).Err())
		}
	}
	require.ErrorIs(t, s.Renew(ctx, c, m.Timing.Lease), ownership.ErrUnknown)
	_, err = s.Acquire(ctx, ownershipCandidate(m, "B", "b"), m.Timing.Lease)
	require.ErrorIs(t, err, ownership.ErrUnknown)
	require.Error(t, p.ProvisionOwnership(ctx, m, 0), "有残存元数据时不能自动补0重建序列")
	runtime, err := p.SubscribeOwnedBatch(ctx, OwnedReliableOptions{Manifest: m, InstanceID: "new", SessionID: "new-boot", NoDueTasks: true, Key: func(*Message) (string, error) { return "A", nil }, Activate: func(context.Context, ownership.Credential) error { return nil }}, func(context.Context, ownership.Credential, []*Message) error { return nil })
	if runtime != nil {
		_ = runtime.Close(ctx)
	}
	require.ErrorIs(t, err, ownership.ErrUnknown, "元数据未知不能返回已成功启动的订阅")
}

func TestRedisOwnershipGatewaySnapshotConvergesWithoutResubscribe(t *testing.T) {
	_, s, m := ownershipFixture(t)
	ctx := context.Background()
	lease := 15 * time.Second
	for _, gw := range []string{"gw-a", "gw-b"} {
		require.NoError(t, s.OpenGateway(ctx, gw, lease))
		require.NoError(t, s.ReplaceGateway(ctx, ownership.GatewaySnapshot{GatewayID: gw, Revision: 1, Lease: lease, References: []ownership.ScopedReference{{Scope: m.Scope, Key: "A", SessionID: "session", Name: "kline"}}}))
	}
	before, err := s.References(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Len(t, before.References, 2)
	require.NoError(t, s.ReplaceGateway(ctx, ownership.GatewaySnapshot{GatewayID: "gw-a", Revision: 2, Lease: lease}))
	err = s.ReplaceGateway(ctx, ownership.GatewaySnapshot{GatewayID: "gw-a", Revision: 1, Lease: lease, References: []ownership.ScopedReference{{Scope: m.Scope, Key: "A", SessionID: "session", Name: "kline"}}})
	require.ErrorIs(t, err, ownership.ErrStaleSnapshot)
	after, err := s.References(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Len(t, after.References, 1)
	require.Equal(t, "gw-b", after.References[0].GatewayID)
	require.Greater(t, after.Revision, before.Revision)
	require.NoError(t, s.CloseGateway(ctx, "gw-b"))
	final, err := s.References(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Empty(t, final.References)
	require.Greater(t, final.Revision, after.Revision)
	view := ownership.NewReferenceView()
	require.NoError(t, view.Apply(final))
	require.True(t, errors.Is(view.Apply(before), ownership.ErrStaleSnapshot))
}

func TestRedisOwnershipRejectsForgedInstanceAndUndeclaredReferenceKey(t *testing.T) {
	_, s, m := ownershipFixture(t)
	ctx := context.Background()
	c, err := s.Acquire(ctx, ownershipCandidate(m, "A", "a"), m.Timing.Lease)
	require.NoError(t, err)
	for _, mutate := range []func(*ownership.Credential){func(c *ownership.Credential) { c.InstanceID = "forged" }, func(c *ownership.Credential) { c.SessionID = "forged" }, func(c *ownership.Credential) { c.Service = "forged" }} {
		forged := c
		mutate(&forged)
		require.Error(t, s.Ready(ctx, forged))
		require.Error(t, s.Validate(ctx, forged, false))
		require.Error(t, s.Release(ctx, forged))
		require.NoError(t, s.Validate(ctx, c, false))
	}
	require.NoError(t, s.OpenGateway(ctx, "gateway", m.Timing.Lease))
	err = s.ReplaceGateway(ctx, ownership.GatewaySnapshot{GatewayID: "gateway", Revision: 1, Lease: m.Timing.Lease, References: []ownership.ScopedReference{{Scope: m.Scope, Key: "not-declared", SessionID: "session", Name: "ref"}}})
	require.ErrorIs(t, err, ownership.ErrUnknown, "引用不能绕过有界声明新建任意key")
}

// 验证满额引用可以原子替换本Gateway自己的会话，损坏序列不能被快照隐式修复。
func TestRedisOwnershipReferencesReplaceAtCapacityAndRejectCorruptSequence(t *testing.T) {
	p := newPoolBudgetProvider(t, 4, 8)
	ctx := context.Background()
	m := ownership.Manifest{Scope: "capacity", Service: "quotes", Group: "quotes", Selector: "v1", Subjects: []string{"one"}, Keys: []string{"A"}, MaxReferences: 1}.Normalize()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	s, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	require.NoError(t, s.OpenGateway(ctx, "gw", time.Minute))
	snap := ownership.GatewaySnapshot{GatewayID: "gw", Revision: 1, Lease: time.Minute, References: []ownership.ScopedReference{{Scope: m.Scope, Key: "A", SessionID: "one", Name: "view"}}}
	require.NoError(t, s.ReplaceGateway(ctx, snap))
	snap.Revision = 2
	snap.References[0].SessionID = "two"
	require.NoError(t, s.ReplaceGateway(ctx, snap), "替换自有引用不增加最终容量")
	got, err := s.References(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Len(t, got.References, 1)
	require.Equal(t, "two", got.References[0].SessionID)
	require.NoError(t, p.client.Set(ctx, p.ownershipBase(m.Scope)+":sequence", "0", 0).Err())
	snap.Revision = 3
	require.ErrorIs(t, s.ReplaceGateway(ctx, snap), ownership.ErrUnknown)
	require.Equal(t, "0", p.client.Get(ctx, p.ownershipBase(m.Scope)+":sequence").Val(), "未知元数据不得因引用写入恢复成伪epoch")
}

// 元数据恢复只允许显式SQL权威下界、原声明和停稳归属；不删消息/组/游标。
func TestRedisOwnershipExplicitRecoveryRequiresQuiescenceAndPositiveFloor(t *testing.T) {
	p, s, m := ownershipFixture(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	recovery, ok := any(p).(interface {
		RecoverOwnership(context.Context, ownership.Manifest, int64) error
	})
	require.True(t, ok, "缺少原地保全恢复epoch下界的正式维护入口")
	c, err := s.Acquire(ctx, ownershipCandidate(m, "A", "old"), m.Timing.Lease)
	require.NoError(t, err)
	require.ErrorIs(t, recovery.RecoverOwnership(ctx, m, 100), ownership.ErrBusy)
	require.NoError(t, s.Release(ctx, c))
	require.NoError(t, p.client.Del(ctx, p.ownershipBase(m.Scope)+":sequence").Err())
	require.ErrorIs(t, recovery.RecoverOwnership(ctx, m, 0), ownership.ErrUnknown)
	wrong := m
	wrong.Selector = "another-contract"
	require.ErrorIs(t, recovery.RecoverOwnership(ctx, wrong, 100), ownership.ErrUnknown)
	require.NoError(t, recovery.RecoverOwnership(ctx, m, 100))
	next, err := s.Acquire(ctx, ownershipCandidate(m, "A", "new"), m.Timing.Lease)
	require.NoError(t, err)
	require.Greater(t, next.Epoch, int64(100))
	require.ErrorIs(t, s.Validate(ctx, c, false), ownership.ErrFenced)
	require.NoError(t, s.Release(ctx, next))
	require.NoError(t, recovery.RecoverOwnership(ctx, m, 1))
	later, err := s.Acquire(ctx, ownershipCandidate(m, "A", "later"), m.Timing.Lease)
	require.NoError(t, err)
	require.Greater(t, later.Epoch, next.Epoch, "恢复下界不得降低现有序列")
}
