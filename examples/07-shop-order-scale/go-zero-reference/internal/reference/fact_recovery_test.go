// 本文件验证不可达p只在远端持久事实证明后清理；无证明时保留现场并拒绝启动恢复。
package reference

import (
	"context"
	"errors"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestFactOrphanRecoveryRequiresProof 验证成功证明与失败证明分别清理或保留原始事实。
func TestFactOrphanRecoveryRequiresProof(t *testing.T) {
	for _, proved := range []bool{false, true} {
		t.Run(map[bool]string{false: "unproved", true: "proved"}[proved], func(t *testing.T) {
			s, err := openPending(t.TempDir())
			require.NoError(t, err)
			defer s.close()
			o, err := s.accept(context.Background(), testOrder(100, "orphan"))
			require.NoError(t, err)
			require.NoError(t, s.db.Update(func(tx *badger.Txn) error { return tx.Delete(queueKey(o)) }))
			verified := false
			err = s.reconcileFactOrphans(func(items []*Order) error {
				verified = true
				require.Len(t, items, 1)
				require.EqualValues(t, 100, items[0].ID)
				if !proved {
					return errors.New("没有远端持久事实证明")
				}
				return nil
			})
			require.True(t, verified)
			if proved {
				require.NoError(t, err)
				require.Zero(t, s.depth.Load())
				item, e := s.find(o.UserID, o.RequestID)
				require.NoError(t, e)
				require.Nil(t, item)
			} else {
				require.Error(t, err)
				require.EqualValues(t, 1, s.depth.Load())
				item, e := s.find(o.UserID, o.RequestID)
				require.NoError(t, e)
				require.NotNil(t, item)
			}
		})
	}
}
