// 本文件显式运行原生接单/确认并发和后台压缩压力，保留独占目录及首次状态异常。
package reference

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestPendingCompactionConcurrentConfirm 验证成功事务计数在合批、重复确认、自然压缩和重开后仍一致。
func TestPendingCompactionConcurrentConfirm(t *testing.T) {
	root := os.Getenv("CORE_TEST_PENDING_STRESS_DIR")
	if root == "" {
		t.Skip("仅显式CORE_TEST_PENDING_STRESS_DIR运行，保留失败目录")
	}
	path := filepath.Join(root, "native-concurrent")
	_, err := os.Stat(path)
	require.True(t, os.IsNotExist(err), "禁止覆盖旧实验目录")
	s, err := openPending(path)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.close()) }()
	for round := 0; round < 64; round++ {
		failures := make(chan error, 5)
		done := make(chan struct{})
		var producers sync.WaitGroup
		for worker := 0; worker < 4; worker++ {
			producers.Go(func() {
				rng := rand.New(rand.NewSource(int64(round*4 + worker)))
				for n := 0; n < 512; n++ {
					id := uint(round*2048 + worker*512 + n + 1)
					order := testOrder(id, fmt.Sprintf("stress-%d", id))
					payload := make([]byte, 1024)
					_, _ = rng.Read(payload)
					order.AddressDetail = base64.StdEncoding.EncodeToString(payload)
					if _, err := s.accept(context.Background(), order); err != nil {
						failures <- err
						return
					}
				}
			})
		}
		consumerDone := make(chan struct{})
		go func() {
			defer close(consumerDone)
			for {
				items, err := s.batch(512)
				if err != nil {
					failures <- err
					return
				}
				if len(items) > 0 {
					if err := s.confirm(items); err != nil {
						failures <- err
						return
					}
					if err := s.confirm(items); err != nil {
						failures <- err
						return
					}
					continue
				}
				select {
				case <-done:
					// done关闭后不再有新提交；重读，避免把关闭前的空快照当作最终排空。
					last, err := s.batch(512)
					if err != nil {
						failures <- err
						return
					}
					if len(last) == 0 {
						return
					}
					if err := s.confirm(last); err != nil {
						failures <- err
						return
					}
				default:
				}
			}
		}()
		producers.Wait()
		close(done)
		<-consumerDone
		close(failures)
		for err := range failures {
			require.NoError(t, err)
		}
		s.mu.Lock()
		counts := map[string]int{}
		err = s.db.View(func(tx *badger.Txn) error {
			it := tx.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			for it.Rewind(); it.Valid(); it.Next() {
				k := it.Item().Key()
				if len(k) >= 2 {
					counts[string(k[:2])]++
				}
			}
			return nil
		})
		depth := s.depth.Load()
		levels := s.db.Levels()
		s.mu.Unlock()
		require.NoError(t, err)
		require.Zero(t, depth, "round=%d counts=%v levels=%v", round, counts, levels)
		require.Zero(t, counts["p:"], "round=%d counts=%v levels=%v", round, counts, levels)
		require.Zero(t, counts["q:"], "round=%d counts=%v levels=%v", round, counts, levels)
		t.Logf("round=%d committed=%d depth=%d", round, (round+1)*2048, depth)
	}
	require.NoError(t, s.close())
	s, err = openPending(path)
	require.NoError(t, err)
	require.Zero(t, s.depth.Load())
}
