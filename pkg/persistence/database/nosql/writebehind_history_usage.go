// 本文件只在受信应用显式查询时扫描本地逻辑占用，不将未采集物理I/O或后台采样伪装为零。
package nosql

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

// WriteBehindHistoryUsage将业务副本、未解决原件、终态载荷、终态元数据和其他技术索引分开核算。
// Bytes包含各类当前可见key/value的编码字节；与Badger物理LSM/VLog、历史版本和分配空间不是同一口径。
type WriteBehindHistoryUsage struct {
	BusinessRecords, UnresolvedRecords, TerminalRecords                                             int64
	BusinessBytes, UnresolvedBytes, TerminalPayloadBytes, TerminalMetadataBytes, OtherMetadataBytes int64
	Pending                                                                                         int64
}

// GetWriteBehindHistoryUsage在同一只读事务获取逻辑快照；成本随本prefix记录数增长，禁止放在Save热路径。
func (p *PrefixedBadgerDB[T]) GetWriteBehindHistoryUsage(ctx context.Context) (WriteBehindHistoryUsage, error) {
	var out WriteBehindHistoryUsage
	if p.IsClosed() {
		return out, ErrWriteStoreClosed
	}
	if p.manager.config.WriteBehindFailure == nil {
		return out, ErrWriteBehindDisposition
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e := p.manager.db.View(func(txn *badger.Txn) error {
		for _, prefix := range []string{p.prefix, p.dispositionPrefix(), p.syncQueuePrefix()} {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.Prefix = []byte(prefix)
			it := txn.NewIterator(opts)
			for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
				if e := ctx.Err(); e != nil {
					it.Close()
					return e
				}
				item := it.Item()
				key := string(item.Key())
				size := int64(len(key)) + int64(item.ValueSize())
				if prefix == p.prefix {
					out.BusinessRecords++
					out.BusinessBytes += size
					continue
				}
				if prefix == p.syncQueuePrefix() {
					out.Pending++
					out.OtherMetadataBytes += size
					continue
				}
				if !strings.HasPrefix(key, prefix+"r:") {
					out.OtherMetadataBytes += size
					continue
				}
				var r struct {
					State    WriteBehindFailureState
					Snapshot struct {
						Item json.RawMessage `json:"item"`
					}
				}
				if e := item.Value(func(b []byte) error { return json.Unmarshal(b, &r) }); e != nil {
					it.Close()
					return e
				}
				if !dispositionTerminal(r.State) {
					out.UnresolvedRecords++
					out.UnresolvedBytes += size
					continue
				}
				out.TerminalRecords++
				payload := int64(len(r.Snapshot.Item))
				if string(r.Snapshot.Item) == "null" {
					payload = 0
				}
				out.TerminalPayloadBytes += payload
				out.TerminalMetadataBytes += size - payload
			}
			it.Close()
		}
		return nil
	})
	return out, e
}
