// 本文件描述已返回target的本地确认命令；仅通过既有FIFO提交，不含SQL或应用回调。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

type writeBehindConfirmation[T types.IModel] struct {
	store                        *PrefixedBadgerDB[T]
	ctx                          context.Context
	selected                     []WriteBehindFailureRecord[T]
	success                      map[string]bool
	cause                        error
	snapshots                    []*SyncQueueItem[T]
	keys                         []string
	notify                       []WriteBehindFailureRecord[T]
	invalid, canceled, commitErr error
	finished                     bool
	bytesBudget                  int
}

// apply只在实际事务内核对原件与结果；失败事务的内存结果在下一次实际事务前清空。
func (work *writeBehindConfirmation[T]) apply(txn *badger.Txn, groups map[string]struct{}) error {
	p := work.store
	work.snapshots = nil
	work.keys = nil
	work.notify = nil
	work.invalid = nil
	work.canceled = nil
	success, cause := work.success, work.cause
	if work.ctx != nil {
		work.canceled = work.ctx.Err()
		if p.IsClosed() || p.disposition.ctx.Err() != nil {
			work.canceled = errors.Join(work.canceled, ErrWriteStoreClosed)
		}
		if work.canceled != nil {
			success = nil
			cause = errors.Join(cause, work.canceled)
		}
	}
	for _, original := range work.selected {
		var r WriteBehindFailureRecord[T]
		if e := dispositionRead(txn, p.dispositionPrefix()+"r:"+original.ID, &r); e != nil {
			if errors.Is(e, badger.ErrKeyNotFound) {
				work.invalid = errors.Join(work.invalid, e)
				continue
			}
			return e
		}
		if r.ID != original.ID || r.Snapshot == nil || r.Snapshot.Key != original.Snapshot.Key || r.State != WriteBehindInFlight || r.Attempts != original.Attempts || r.BatchID != original.BatchID || r.Fingerprint != original.Fingerprint || !r.Snapshot.UpdatedAt.Equal(original.Snapshot.UpdatedAt) {
			work.invalid = errors.Join(work.invalid, ErrWriteBehindDisposition)
			continue // 语义失效不是存储失败，不回滚无关的有效原版本。
		}
		if success[r.Snapshot.Key] {
			removed, e := p.completeDisposition(txn, &r, WriteBehindCompleted, groups)
			if e != nil {
				return e
			}
			if removed {
				work.snapshots = append(work.snapshots, r.Snapshot)
				work.keys = append(work.keys, r.Snapshot.Key)
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
		work.notify = append(work.notify, r)
	}

	return nil
}

// commitConfirmationCommands复用同prefix互斥与真实Badger事务；TooBig仅拆本地写集，不重调target。
func (p *PrefixedBadgerDB[T]) commitConfirmationCommands(commands []*writeBehindConfirmation[T]) {
	items := 0
	for _, c := range commands {
		items += len(c.selected)
	}
	err := p.updateLocalTransactionDirect(localCommitConfirmation, items, func(txn *badger.Txn) error {
		groups := make(map[string]struct{}, items)
		for _, c := range commands {
			if e := c.apply(txn, groups); e != nil {
				return e
			}
		}
		return p.deleteEmptyDispositionGroups(txn, groups)
	})
	if errors.Is(err, badger.ErrTxnTooBig) && len(commands) > 1 {
		middle := len(commands) / 2
		p.commitConfirmationCommands(commands[:middle])
		p.commitConfirmationCommands(commands[middle:])
		return
	}
	for _, c := range commands {
		c.commitErr = err
		c.finished = true
	}
}

func (work *writeBehindConfirmation[T]) cost() (int, error) {
	size := 0
	for _, r := range work.selected {
		b, e := json.Marshal(&r)
		if e != nil {
			return 0, e
		}
		size += 2*len(b) + 4096
	}
	if work.cause != nil {
		size += len(work.cause.Error()) * len(work.selected)
	}
	return size, nil
}
