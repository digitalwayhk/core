// 本文件验证崩溃恢复必须有外部隔离证明且精确匹配旧世代。
package identity

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRecoverStoppedBootRequiresExactProof(t *testing.T) {
	ctx := context.Background()
	a := NewAuthority(NewMemoryStore(), "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	epoch, err := a.AcquireBoot(ctx, b.Key, "boot-a", "", false)
	require.NoError(t, err)
	require.NoError(t, a.Reserve(ctx, b.Key, "boot-a", epoch, 100))
	p := RecoveryProof{Key: b.Key, Epoch: "epoch-a", BootID: "boot-a", BootEpoch: epoch, ReservedUntil: 100}
	require.Error(t, a.RecoverStoppedBoot(ctx, p, false))
	p.StoppedEvidence = "测试进程已退出且卷隔离"
	p.PendingEvidence = "原路径保留并已核对"
	require.NoError(t, a.RecoverStoppedBoot(ctx, p, true))
	require.NoError(t, a.VerifyBoot(ctx, b.Key, "boot-a", epoch))
	p.ReservedUntil = 99
	require.Error(t, a.RecoverStoppedBoot(ctx, p, false))
	p.ReservedUntil = 100
	require.NoError(t, a.RecoverStoppedBoot(ctx, p, false))
	second, err := a.AcquireBoot(ctx, b.Key, "boot-b", "boot-a", false)
	require.NoError(t, err)
	require.Greater(t, second, epoch)
	require.Error(t, a.RecoverStoppedBoot(ctx, p, false))
	require.Error(t, a.Reserve(ctx, b.Key, "boot-a", epoch, 101))
}
