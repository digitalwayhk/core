// 本文件验证计划G2的列表边界：缓存计数异常不导致panic或跨用户数据泄露。
package reference

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestOrdersWithUntrustedPendingDepth 用GORM只生成远端查询，独立验证真实本地列表与用户隔离。
func TestOrdersWithUntrustedPendingDepth(t *testing.T) {
	for _, depth := range []int64{-1, 0, 1 << 60} {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			s, err := openPending(t.TempDir())
			require.NoError(t, err)
			defer s.close()
			_, err = s.accept(context.Background(), testOrder(100, "mine"))
			require.NoError(t, err)
			other := testOrder(101, "other")
			other.UserID++
			_, err = s.accept(context.Background(), other)
			require.NoError(t, err)
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DryRun: true})
			require.NoError(t, err)
			pool, err := db.DB()
			require.NoError(t, err)
			defer pool.Close()
			s.depth.Store(depth)
			app := &application{db: db, local: s}
			require.NotPanics(t, func() {
				items, err := app.orders(720001)
				if depth < 0 {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				require.Len(t, items, 1)
				require.EqualValues(t, 100, items[0].ID)
			})
		})
	}
}

// TestNegativeDepthRejectsNewAdmission 验证发现无效状态后停止新接单并保留已有事实。
func TestNegativeDepthRejectsNewAdmission(t *testing.T) {
	s, err := openPending(t.TempDir())
	require.NoError(t, err)
	defer s.close()
	s.depth.Store(-1)
	_, err = s.accept(context.Background(), testOrder(100, "invalid-state"))
	require.Error(t, err)
	items, err := s.batch(512)
	require.NoError(t, err)
	require.Empty(t, items)
}
