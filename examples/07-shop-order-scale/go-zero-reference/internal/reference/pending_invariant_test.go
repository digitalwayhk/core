// 本文件核对静止检查点的内存计数、事实与双向索引，不把基本契约当作压缩异常复现。
package reference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dgraph-io/badger/v3"
	"github.com/stretchr/testify/require"
)

// TestPendingQuiescentInvariant 验证暂停写入后的计数及双向索引，以及重复确认不重复扣减。
func TestPendingQuiescentInvariant(t *testing.T) {
	s, err := openPending(t.TempDir())
	require.NoError(t, err)
	defer func() { require.NoError(t, s.close()) }()
	item, err := s.accept(context.Background(), testOrder(100, "invariant"))
	require.NoError(t, err)
	s.mu.Lock()
	depth := s.depth.Load()
	facts, queues := int64(0), int64(0)
	err = s.db.View(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			key := it.Item().KeyCopy(nil)
			if bytes.HasPrefix(key, []byte("p:")) {
				facts++
				var order Order
				if err := it.Item().Value(func(v []byte) error { return json.Unmarshal(v, &order) }); err != nil {
					return err
				}
				queue, err := tx.Get(queueKey(&order))
				if err != nil {
					return err
				}
				value, err := queue.ValueCopy(nil)
				if err != nil {
					return err
				}
				if !bytes.Equal(value, key) || !bytes.Equal(key, localKey(&order)) {
					return errors.New("事实缺少匹配队列索引")
				}
			}
			if !bytes.HasPrefix(key, []byte("q:")) {
				continue
			}
			queues++
			dataKey, err := it.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			value, err := tx.Get(dataKey)
			if err != nil {
				return err
			}
			var order Order
			if err := value.Value(func(v []byte) error { return json.Unmarshal(v, &order) }); err != nil {
				return err
			}
			if !bytes.Equal(key, queueKey(&order)) || !bytes.Equal(dataKey, localKey(&order)) {
				return errors.New("队列索引与事实身份不一致")
			}
		}
		return nil
	})
	s.mu.Unlock()
	require.NoError(t, err)
	require.GreaterOrEqual(t, depth, int64(0))
	require.Equal(t, facts, queues)
	require.Equal(t, facts, depth)
	require.NoError(t, s.confirm([]*Order{item}))
	require.NoError(t, s.confirm([]*Order{item}))
	require.Zero(t, s.depth.Load())
}
