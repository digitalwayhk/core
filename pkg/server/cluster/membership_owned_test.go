// 本文件验证稳定 membership 会恢复节点并在退出后停止恢复。
package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestMembershipRecoversMissingRegistration 验证恢复不需要从已丢失的节点读取身份。
func TestMembershipRecoversMissingRegistration(t *testing.T) {
	p := NewLocalProvider(time.Second, time.Second, time.Second)
	defer p.Close()
	n := &NodeInfo{ID: "stable", ServiceName: "orders", ServiceInstanceID: "a", Address: "127.0.0.1", Port: 18000}
	o := RegistrationOwner{BootID: "boot-a", Epoch: 1}
	require.NoError(t, p.AcquireRegistration(context.Background(), n, o))
	require.NoError(t, p.ReleaseRegistration(context.Background(), n, o))
	m := NewMembershipManager(p, n.ID, 5*time.Millisecond, WithOwnedRegistration(n, o, nil, nil))
	m.Start(context.Background())
	require.Eventually(t, func() bool { _, err := p.Get(context.Background(), n.ID); return err == nil }, time.Second, time.Millisecond)
	require.NoError(t, m.Stop(context.Background()))
	_, err := p.Get(context.Background(), n.ID)
	require.ErrorIs(t, err, ErrNodeNotFound)
}

// TestMembershipRecoveryDelayBounded 验证退避预算和抖动范围。
func TestMembershipRecoveryDelayBounded(t *testing.T) {
	for failures := 1; failures < 100; failures++ {
		d := membershipRecoveryDelay(time.Second, failures)
		require.GreaterOrEqual(t, d, 750*time.Millisecond)
		require.LessOrEqual(t, d, 30*time.Second)
	}
}

// TestMigrationDoesNotDeleteIndependentStableOwner 验证迁移不能从缺失快照推断目标实例已停机。
func TestMigrationDoesNotDeleteIndependentStableOwner(t *testing.T) {
	p := NewLocalProvider(time.Second, time.Second, time.Second)
	defer p.Close()
	n := &NodeInfo{ID: "target-only", ServiceName: "orders", ServiceInstanceID: "new"}
	o := RegistrationOwner{BootID: "boot-new", Epoch: 2}
	require.NoError(t, p.AcquireRegistration(context.Background(), n, o))
	require.NoError(t, reconcileProviderSnapshot(context.Background(), p, "orders", nil))
	current, err := p.Get(context.Background(), n.ID)
	require.NoError(t, err)
	require.Equal(t, o.BootID, current.StableBootID)
}
