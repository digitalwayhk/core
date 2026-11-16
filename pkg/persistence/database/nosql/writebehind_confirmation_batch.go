// 本文件仅批量提交已有target结果；不在本地事务内调用target或应用回调。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"

	"github.com/dgraph-io/badger/v4"
)

const dispositionConfirmationMaxItems = 32
const dispositionConfirmationMaxBytes = 1 << 20

// commitDispositionResults 在条数/编码字节预算内落地；真实事务失败不发布该块任何内存结果。
func (p *PrefixedBadgerDB[T]) commitDispositionResults(selected []WriteBehindFailureRecord[T], success map[string]bool, cause error, contexts ...context.Context) (int, []WriteBehindFailureRecord[T], error) {
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
		n, notices, err := p.commitDispositionResultBlockBudget(selected[start:end], success, cause, size, contexts...)
		confirmed += n
		notify = append(notify, notices...)
		resultErr = errors.Join(resultErr, err)
		start = end
		// 每块归还元数据互斥及执行机会；不连续持锁覆盖整个积压。
		runtime.Gosched()
	}
	return confirmed, notify, resultErr
}

func (p *PrefixedBadgerDB[T]) commitDispositionResultBlock(selected []WriteBehindFailureRecord[T], success map[string]bool, cause error, contexts ...context.Context) (int, []WriteBehindFailureRecord[T], error) {
	return p.commitDispositionResultBlockBudget(selected, success, cause, 0, contexts...)
}

// commitDispositionResultBlockBudget复用上游已编码的同一快照预算，避免确认准备再次完整编码业务T。
func (p *PrefixedBadgerDB[T]) commitDispositionResultBlockBudget(selected []WriteBehindFailureRecord[T], success map[string]bool, cause error, budget int, contexts ...context.Context) (int, []WriteBehindFailureRecord[T], error) {
	work := &writeBehindConfirmation[T]{store: p, selected: selected, success: success, cause: cause, bytesBudget: budget}
	if len(contexts) > 0 {
		work.ctx = contexts[0]
	}
	var err error
	if work.ctx != nil && p.localConfirmationSubmit != nil {
		// 完整编码预算在入队前计算，不能占用唯一提交worker；仍以同一原快照估算并保留实际Badger硬限。
		if work.bytesBudget <= 0 {
			work.bytesBudget, err = work.cost()
			if err != nil {
				return 0, nil, err
			}
		}
		err = p.localConfirmationSubmit(work)
	} else {
		err = p.updateWriteBehindTransactionKind(localCommitConfirmation, len(selected), func(txn *badger.Txn) error {
			groups := make(map[string]struct{}, len(selected))
			if e := work.apply(txn, groups); e != nil {
				return e
			}
			return p.deleteEmptyDispositionGroups(txn, groups)
		})
	}
	if errors.Is(err, badger.ErrTxnTooBig) && len(selected) > 1 {
		middle := len(selected) / 2
		n, left, e1 := p.commitDispositionResultBlock(selected[:middle], success, cause, contexts...)
		runtime.Gosched()
		m, right, e2 := p.commitDispositionResultBlock(selected[middle:], success, cause, contexts...)
		return n + m, append(left, right...), errors.Join(e1, e2)
	}
	if err != nil {
		return 0, nil, err
	}
	p.incrementPendingCount(-len(work.snapshots))
	if observer := p.disposition.confirmationObserver; observer != nil {
		observer(work.snapshots)
	}
	p.scheduleSyncAfterDelete(p.collectSyncAfterDeleteCandidates(work.snapshots, work.keys))
	return len(work.snapshots), work.notify, errors.Join(work.invalid, work.canceled)
}
