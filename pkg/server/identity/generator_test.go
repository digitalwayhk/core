// 本文件使用受控时钟验证跨重启时间保护和先持久后返回。
package identity

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fixedSource struct{ id int64 }

func (s fixedSource) NextId() int64 { return s.id }

// TestGeneratorRestartTimeFence 验证时钟未越过旧预留范围时新 worker 不可用。
func TestGeneratorRestartTimeFence(t *testing.T) {
	ctx := context.Background()
	a := NewAuthority(NewMemoryStore(), "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	epoch, err := a.AcquireBoot(ctx, b.Key, "boot-a", "", false)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "identity.json")
	require.NoError(t, Create(path, "orders", "a"))
	f, err := Open(path, "orders", "a")
	require.NoError(t, err)
	defer f.Close()
	now := time.UnixMilli(1800000000000)
	id := (now.UnixMilli()-GeneratorBaseTime)<<12 | int64(b.WorkerID)<<6 | 5
	g, err := NewGenerator(ctx, a, f, b, "boot-a", epoch, GeneratorOptions{Now: func() time.Time { return now }, Source: fixedSource{id}, Window: time.Second})
	require.NoError(t, err)
	require.Equal(t, id, g.NextId())
	current, err := a.Get(ctx, b.Key)
	require.NoError(t, err)
	require.Greater(t, current.ReservedUntil, now.UnixMilli())
	_, err = NewGenerator(ctx, a, f, b, "boot-a", epoch, GeneratorOptions{Now: func() time.Time { return now }})
	require.Error(t, err, "未等待的新 worker 不应重用旧范围")
}

// TestGeneratorReservationBeforeReturn 验证超前逻辑时间也先持久预留，失去世代则拒绝返回。
func TestGeneratorReservationBeforeReturn(t *testing.T) {
	ctx := context.Background()
	a := NewAuthority(NewMemoryStore(), "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	epoch, err := a.AcquireBoot(ctx, b.Key, "boot-a", "", false)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "identity.json")
	require.NoError(t, Create(path, "orders", "a"))
	f, err := Open(path, "orders", "a")
	require.NoError(t, err)
	defer f.Close()
	now := time.UnixMilli(1800000000000)
	future := now.Add(3 * time.Second).UnixMilli()
	id := (future-GeneratorBaseTime)<<12 | int64(b.WorkerID)<<6 | 5
	g, err := NewGenerator(ctx, a, f, b, "boot-a", epoch, GeneratorOptions{Now: func() time.Time { return now }, Source: fixedSource{id}, Window: time.Second})
	require.NoError(t, err)
	require.Equal(t, id, g.NextId())
	require.GreaterOrEqual(t, f.State.ReservedUntil, future)
	require.NoError(t, a.ReleaseBoot(ctx, b.Key, "boot-a", epoch))
	g.Guard = func() error { return a.VerifyBoot(ctx, b.Key, "boot-a", epoch) }
	require.Panics(t, func() { g.NextId() })
}

// TestGeneratorTemporaryAdmissionRecovers 验证暂时失去准入不会永久关闭生成器。
func TestGeneratorTemporaryAdmissionRecovers(t *testing.T) {
	ctx := context.Background()
	a := NewAuthority(NewMemoryStore(), "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	epoch, err := a.AcquireBoot(ctx, b.Key, "boot-a", "", false)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "identity.json")
	require.NoError(t, Create(path, "orders", "a"))
	f, err := Open(path, "orders", "a")
	require.NoError(t, err)
	defer f.Close()
	now := time.UnixMilli(1800000000000)
	id := (now.UnixMilli()-GeneratorBaseTime)<<12 | int64(b.WorkerID)<<6 | 5
	g, err := NewGenerator(ctx, a, f, b, "boot-a", epoch, GeneratorOptions{Now: func() time.Time { return now }, Source: fixedSource{id}})
	require.NoError(t, err)
	g.Guard = func() error { return ErrAdmissionUnavailable }
	require.Panics(t, func() { g.NextId() })
	g.Guard = nil
	require.NotPanics(t, func() { require.Equal(t, id, g.NextId()) })
}
