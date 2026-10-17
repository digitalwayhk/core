// 本文件验证时钟异常、持久写入失败及权威旧备份不会产生无保护编号。
package identity

import (
	"context"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
	"time"
)

func TestGeneratorClockAndPersistenceFailures(t *testing.T) {
	for _, fault := range []string{"clock", "local-save", "authority-loss", "wrong-worker"} {
		t.Run(fault, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryStore()
			a := NewAuthority(store, "epoch-a")
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
			if fault == "local-save" || fault == "authority-loss" {
				id += int64(3*time.Second/time.Millisecond) << 12
			}
			if fault == "wrong-worker" {
				id ^= 1 << 6
			}
			g, err := NewGenerator(ctx, a, f, b, "boot-a", epoch, GeneratorOptions{Now: func() time.Time { return now }, Source: fixedSource{id}})
			require.NoError(t, err)
			switch fault {
			case "clock":
				now = now.Add(-time.Second)
			case "local-save":
				require.NoError(t, f.Close())
			case "authority-loss":
				raw, e := store.Load(ctx)
				require.NoError(t, e)
				ok, e := store.CompareAndSwap(ctx, raw, "")
				require.NoError(t, e)
				require.True(t, ok)
			}
			require.Panics(t, func() { g.NextId() })
			require.Panics(t, func() { g.NextId() }, "永久故障不能继续生成")
		})
	}
}
