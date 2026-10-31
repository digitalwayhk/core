// 本文件仅批量提交已有target结果；不在本地事务内调用target或应用回调。
package nosql

import (
	"encoding/json"
	"errors"
	"runtime"
	"time"

	"github.com/dgraph-io/badger/v4"
)

const dispositionConfirmationMaxItems = 32
const dispositionConfirmationMaxBytes = 1 << 20

// commitDispositionResults 在条数/编码字节预算内落地；真实事务失败不发布该块任何内存结果。
func (p *PrefixedBadgerDB[T]) commitDispositionResults(selected []WriteBehindFailureRecord[T], success map[string]bool, cause error) (int, []WriteBehindFailureRecord[T], error) {
	confirmed := 0
	var notify []WriteBehindFailureRecord[T]
	var resultErr error
	for start := 0; start < len(selected); {
		end, size := start, 0
		maxItems := dispositionConfirmationMaxItems
		if p.localConfirmationLimit > 0 && p.localConfirmationLimit < maxItems {
			maxItems = p.localConfirmationLimit
		}
		for end < len(selected) && end-start < maxItems {
			b, err := json.Marshal(&selected[end])
			if err != nil {
				return confirmed, notify, errors.Join(resultErr, err)
			}
			// 包括生命周期原件、当前模型副本及索引开销；Badger还独立核验实际写集硬限。
			cost := 2*len(b) + 4096
			if cause != nil {
				cost += len(cause.Error())
			}
			if end > start && size+cost > dispositionConfirmationMaxBytes {
				break
			}
			size += cost
			end++
		}
		n, notices, err := p.commitDispositionResultBlock(selected[start:end], success, cause)
		confirmed += n
		notify = append(notify, notices...)
		resultErr = errors.Join(resultErr, err)
		start = end
		// 每块归还元数据互斥及执行机会；不连续持锁覆盖整个积压。
		runtime.Gosched()
	}
	return confirmed, notify, resultErr
}

func (p *PrefixedBadgerDB[T]) commitDispositionResultBlock(selected []WriteBehindFailureRecord[T], success map[string]bool, cause error) (int, []WriteBehindFailureRecord[T], error) {
	var snapshots []*SyncQueueItem[T]
	var keys []string
	var notify []WriteBehindFailureRecord[T]
	var invalid error
	groups := make(map[string]struct{}, len(selected))
	err := p.updateWriteBehindTransactionKind(localCommitConfirmation, len(selected), func(txn *badger.Txn) error {
		for _, original := range selected {
			var r WriteBehindFailureRecord[T]
			if e := dispositionRead(txn, p.dispositionPrefix()+"r:"+original.ID, &r); e != nil {
				if errors.Is(e, badger.ErrKeyNotFound) {
					invalid = errors.Join(invalid, e)
					continue
				}
				return e
			}
			if r.ID != original.ID || r.Snapshot == nil || r.Snapshot.Key != original.Snapshot.Key || r.State != WriteBehindInFlight || r.Attempts != original.Attempts || r.Fingerprint != original.Fingerprint || !r.Snapshot.UpdatedAt.Equal(original.Snapshot.UpdatedAt) {
				invalid = errors.Join(invalid, ErrWriteBehindDisposition)
				continue // 语义失效不是存储失败，不回滚无关的有效原版本。
			}
			if success[r.Snapshot.Key] {
				removed, e := p.completeDisposition(txn, &r, WriteBehindCompleted, groups)
				if e != nil {
					return e
				}
				if removed {
					snapshots = append(snapshots, r.Snapshot)
					keys = append(keys, r.Snapshot.Key)
				}
				continue
			}
			r.State = WriteBehindUnknown
			r.Reason = "target_outcome_unknown"
			if r.FirstFailureAt.IsZero() {
				r.FirstFailureAt = time.Now().UTC()
			}
			if cause != nil {
				r.ErrorMessage = cause.Error()
			} else {
				r.ErrorMessage = ErrWriteBehindNoProgress.Error()
			}
			if e := p.saveDisposition(txn, &r); e != nil {
				return e
			}
			notify = append(notify, r)
		}
		return p.deleteEmptyDispositionGroups(txn, groups)
	})
	if errors.Is(err, badger.ErrTxnTooBig) && len(selected) > 1 {
		// 仅重建纯本地写集；未提交块的快照、计数及通知全部丢弃。不会重调target。
		middle := len(selected) / 2
		n, left, e1 := p.commitDispositionResultBlock(selected[:middle], success, cause)
		runtime.Gosched()
		m, right, e2 := p.commitDispositionResultBlock(selected[middle:], success, cause)
		return n + m, append(left, right...), errors.Join(e1, e2)
	}
	if err != nil {
		return 0, nil, err
	}
	p.incrementPendingCount(-len(snapshots))
	p.scheduleSyncAfterDelete(p.collectSyncAfterDeleteCandidates(snapshots, keys))
	return len(snapshots), notify, invalid
}
