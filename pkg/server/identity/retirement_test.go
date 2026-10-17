// 本文件验证显式退役需要停机证据，不能回收活跃编号。
package identity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratorRetirementAndSafeReuse 验证 dry-run 不写、旧身份永久禁用及安全复用。
func TestGeneratorRetirementAndSafeReuse(t *testing.T) {
	ctx := context.Background()
	a := NewAuthority(NewMemoryStore(), "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	proof := RetirementProof{Key: b.Key, Epoch: "epoch-a", BootEpoch: 0, ReservedUntil: 0, StoppedEvidence: "独占测试进程已退出", PendingEvidence: "测试无业务 pending"}
	require.NoError(t, a.Retire(ctx, proof, true))
	current, err := a.Get(ctx, b.Key)
	require.NoError(t, err)
	require.False(t, current.Retired)
	require.NoError(t, a.Retire(ctx, proof, false))
	_, err = a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.Error(t, err)
	other, err := a.Bind(ctx, "orders", "b", 1, 0, 31, false)
	require.NoError(t, err)
	require.Equal(t, b.WorkerID, other.WorkerID)
}

// TestGeneratorRetirementRequiresProof 验证活跃世代、证据缺失及时间未过均拒绝。
func TestGeneratorRetirementRequiresProof(t *testing.T) {
	ctx := context.Background()
	a := NewAuthority(NewMemoryStore(), "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	proof := RetirementProof{Key: b.Key, Epoch: "epoch-a", StoppedEvidence: "已隔离", PendingEvidence: "已处理"}
	epoch, err := a.AcquireBoot(ctx, b.Key, "boot-a", "", false)
	require.NoError(t, err)
	proof.BootEpoch = epoch
	require.Error(t, a.Retire(ctx, proof, false))
	require.NoError(t, a.ReleaseBoot(ctx, b.Key, "boot-a", epoch))
	proof.PendingEvidence = ""
	require.Error(t, a.Retire(ctx, proof, false))
	current, err := a.Get(ctx, b.Key)
	require.NoError(t, err)
	require.False(t, current.Retired)
}
