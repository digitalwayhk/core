// 本文件在既有本地提交边界计量有限种类的锁等待与事务，不增加观测线程或业务标签。
package nosql

import (
	"time"

	"github.com/dgraph-io/badger/v4"
)

// LocalTransactionMetrics 为累计本地事务快照，CommitDuration包含编码/Badger提交，不是纯fsync。
type LocalTransactionMetrics struct {
	Attempts          uint64
	Committed         uint64
	Failures          uint64
	Items             uint64
	MaxItems          int
	TooBig            uint64
	QueueWait         time.Duration
	LockWait          time.Duration
	CommitDuration    time.Duration
	MaxCommitDuration time.Duration
}

type localCommitKind uint8

const (
	localCommitMetadata localCommitKind = iota
	localCommitForeground
	localCommitConfirmation
)

func (p *PrefixedBadgerDB[T]) updateWriteBehindTransactionKind(kind localCommitKind, items int, fn func(*badger.Txn) error) error {
	if kind != localCommitForeground && p.localSubmit != nil {
		queued := time.Now()
		return p.localSubmit(func() error {
			p.syncMetricsMu.Lock()
			if kind == localCommitConfirmation {
				p.syncMetrics.ConfirmationLocal.QueueWait += time.Since(queued)
			} else {
				p.syncMetrics.MetadataLocal.QueueWait += time.Since(queued)
			}
			p.syncMetricsMu.Unlock()
			return p.updateLocalTransactionDirect(kind, items, fn)
		})
	}
	return p.updateLocalTransactionDirect(kind, items, fn)
}

func (p *PrefixedBadgerDB[T]) updateLocalTransactionDirect(kind localCommitKind, items int, fn func(*badger.Txn) error) error {
	start := time.Now()
	var locked time.Time
	var err error
	func() {
		if p.disposition != nil {
			p.disposition.storageMu.Lock()
			defer p.disposition.storageMu.Unlock()
		}
		locked = time.Now()
		err = p.manager.db.Update(fn)
	}()
	duration := time.Since(locked)
	p.syncMetricsMu.Lock()
	m := &p.syncMetrics.MetadataLocal
	if kind == localCommitForeground {
		m = &p.syncMetrics.ForegroundLocal
	}
	if kind == localCommitConfirmation {
		m = &p.syncMetrics.ConfirmationLocal
	}
	m.Attempts++
	m.LockWait += locked.Sub(start)
	m.CommitDuration += duration
	if duration > m.MaxCommitDuration {
		m.MaxCommitDuration = duration
	}
	if err != nil {
		m.Failures++
		if err == badger.ErrTxnTooBig {
			m.TooBig++
		}
	} else {
		m.Committed++
		m.Items += uint64(items)
		if items > m.MaxItems {
			m.MaxItems = items
		}
	}
	p.syncMetricsMu.Unlock()
	return err
}
