// 本文件持久保存失败生命周期、组首索引及版本确认，原始快照不设TTL、不自动删除。
package nosql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

func dispositionHash(v string) string { s := sha256.Sum256([]byte(v)); return hex.EncodeToString(s[:]) }
func (p *PrefixedBadgerDB[T]) dispositionPrefix() string {
	return "\x00wb2:" + dispositionHash(p.prefix) + ":"
}
func dispositionRead(txn *badger.Txn, key string, v any) error {
	item, e := txn.Get([]byte(key))
	if e != nil {
		return e
	}
	return item.Value(func(b []byte) error { return json.Unmarshal(b, v) })
}
func dispositionWrite(txn *badger.Txn, key string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return txn.Set([]byte(key), b)
}
func dispositionTerminal(s WriteBehindFailureState) bool {
	return s == WriteBehindCompleted || s == WriteBehindReleased || s == WriteBehindSuperseded
}
func dispositionGroup[T types.IModel](r *WriteBehindFailureRecord[T]) string {
	if r.Group != "" {
		return dispositionHash("group:" + r.Group)
	}
	return dispositionHash("key:" + r.Snapshot.Key)
}
func (p *PrefixedBadgerDB[T]) dispositionQueueKey(r *WriteBehindFailureRecord[T]) string {
	return fmt.Sprintf("%sq:%s:%020d:%s", p.dispositionPrefix(), dispositionGroup(r), r.Sequence, r.ID)
}

func (p *PrefixedBadgerDB[T]) legacyDispositionCheck() error {
	if p.manager.config.WriteBehindFailure != nil {
		return ErrWriteBehindDisposition
	}
	return p.manager.db.View(func(txn *badger.Txn) error {
		_, e := txn.Get([]byte(p.dispositionPrefix() + "mode"))
		if e == nil {
			return ErrWriteBehindDisposition
		}
		if errors.Is(e, badger.ErrKeyNotFound) {
			return nil
		}
		return e
	})
}

func (p *PrefixedBadgerDB[T]) ensureDispositionEntry(txn *badger.Txn, key string) (err error) {
	rt := p.disposition
	if rt == nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			err = ErrWriteBehindDisposition
		}
	}()
	var snapshot SyncQueueItem[T]
	if err = dispositionRead(txn, key, &snapshot); err != nil {
		return err
	}
	if snapshot.IsSynced || snapshot.HandedOff {
		return nil
	}
	b, err := json.Marshal(&snapshot)
	if err != nil {
		return err
	}
	fp := dispositionHash(string(b))
	id := dispositionHash(rt.policy.StoreID + "\x00" + p.prefix + "\x00" + key + "\x00" + fp)
	base := p.dispositionPrefix()
	if _, err = txn.Get([]byte(base + "r:" + id)); err == nil {
		return nil
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	group, err := rt.handler.OrderingGroup(&snapshot)
	if err != nil {
		return err
	}
	if len(group) > 1024 {
		return ErrWriteBehindDisposition
	}
	var sequence uint64
	if err = dispositionRead(txn, base+"sequence", &sequence); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	sequence++
	if err = dispositionWrite(txn, base+"sequence", sequence); err != nil {
		return err
	}
	var previous string
	if err = dispositionRead(txn, base+"key:"+dispositionHash(key), &previous); err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	if previous != "" {
		var old WriteBehindFailureRecord[T]
		if err = dispositionRead(txn, base+"r:"+previous, &old); err != nil {
			return err
		}
		// 未投递版本仍按原合并语义替换；在途/失败版本永久保留其原始恢复责任。
		if old.State == WriteBehindReady && old.Attempts == 0 {
			old.State = WriteBehindSuperseded
			if err = p.saveDisposition(txn, &old); err != nil {
				return err
			}
		}
	}
	r := WriteBehindFailureRecord[T]{ID: id, Store: rt.policy.StoreID, Fingerprint: fp, Snapshot: &snapshot, Group: group, Sequence: sequence, PreviousID: previous, State: WriteBehindReady, Scope: WriteBehindScopeKey}
	if err = p.saveDisposition(txn, &r); err != nil {
		return err
	}
	if err = dispositionWrite(txn, base+"key:"+dispositionHash(key), id); err != nil {
		return err
	}
	return nil
}

func (p *PrefixedBadgerDB[T]) saveDisposition(txn *badger.Txn, r *WriteBehindFailureRecord[T]) error {
	base := p.dispositionPrefix()
	group := dispositionGroup(r)
	if err := dispositionWrite(txn, base+"r:"+r.ID, r); err != nil {
		return err
	}
	if dispositionTerminal(r.State) {
		if err := txn.Delete([]byte(p.dispositionQueueKey(r))); err != nil {
			return err
		}
		// 只有未解决组才保留轮转标记；归档不会占满调度扫描预算。
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte(base + "q:" + group + ":")
		it.Seek(prefix)
		if !it.ValidForPrefix(prefix) {
			if err := txn.Delete([]byte(base + "g:" + group)); err != nil {
				return err
			}
		}
	} else {
		if err := txn.Set([]byte(base+"g:"+group), nil); err != nil {
			return err
		}
		if err := dispositionWrite(txn, p.dispositionQueueKey(r), r.ID); err != nil {
			return err
		}
	}
	if r.Scope == WriteBehindScopeTarget && !dispositionTerminal(r.State) {
		return dispositionWrite(txn, base+"target:"+r.ID, r.ID)
	}
	return txn.Delete([]byte(base + "target:" + r.ID))
}

// ListWriteBehindFailures 分页返回含原件的生命周期记录，limit上限1000；不应直接暴露为匿名管理API。
func (p *PrefixedBadgerDB[T]) ListWriteBehindFailures(after string, limit int) ([]WriteBehindFailureRecord[T], string, error) {
	if p.disposition == nil {
		return nil, "", ErrWriteBehindDisposition
	}
	if limit < 1 || limit > 1000 {
		return nil, "", ErrInvalidSyncLimit
	}
	var out []WriteBehindFailureRecord[T]
	next := ""
	base := p.dispositionPrefix() + "r:"
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		seek := base + after
		it.Seek([]byte(seek))
		if after != "" && it.Valid() && string(it.Item().Key()) == seek {
			it.Next()
		}
		for ; it.ValidForPrefix([]byte(base)); it.Next() {
			if len(out) == limit {
				next = out[len(out)-1].ID
				break
			}
			var r WriteBehindFailureRecord[T]
			if err := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &r) }); err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	return out, next, err
}

// GetWriteBehindDispositionMetrics 只扫描轻量持久组首索引，不将业务身份作为标签；全量计数按调用方显式查询。
// 不读取已归档快照；内存恒定，扫描成本随未解决组数增加，不属于worker有界选批路径。
func (p *PrefixedBadgerDB[T]) GetWriteBehindDispositionMetrics() (WriteBehindDispositionMetrics, error) {
	var m WriteBehindDispositionMetrics
	if p.disposition == nil {
		return m, ErrWriteBehindDisposition
	}
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		base := []byte(p.dispositionPrefix() + "q:")
		for it.Seek(base); it.ValidForPrefix(base); it.Next() {
			var id string
			if err := it.Item().Value(func(b []byte) error { return json.Unmarshal(b, &id) }); err != nil {
				return err
			}
			var r WriteBehindFailureRecord[T]
			if err := dispositionRead(txn, p.dispositionPrefix()+"r:"+id, &r); err != nil {
				return err
			}
			switch r.State {
			case WriteBehindReady:
				m.Ready++
			case WriteBehindRetrying:
				m.Retrying++
			case WriteBehindUnknown, WriteBehindInFlight, WriteBehindHandling:
				m.Unknown++
			case WriteBehindPaused:
				m.Paused++
			case WriteBehindHandedOff:
				m.HandedOff++
			}
			if r.Reason == "target_budget_exhausted" || r.Reason == "handler_budget_exhausted" {
				m.BudgetExhausted++
			}
			if !r.FirstFailureAt.IsZero() && (m.OldestWaitingAt.IsZero() || r.FirstFailureAt.Before(m.OldestWaitingAt)) {
				m.OldestWaitingAt = r.FirstFailureAt
			}
		}
		return nil
	})
	p.disposition.activeIDs.Range(func(_, _ any) bool { m.HandlerActive++; return true })
	m.HandlerQueued = len(p.disposition.jobs)
	m.HandlerLimit = p.disposition.policy.HandlerWorkers
	return m, err
}

func (p *PrefixedBadgerDB[T]) completeDisposition(txn *badger.Txn, r *WriteBehindFailureRecord[T], state WriteBehindFailureState) (bool, error) {
	var current SyncQueueItem[T]
	removed := false
	err := dispositionRead(txn, r.Snapshot.Key, &current)
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return false, err
	}
	if err == nil && !current.IsSynced && current.UpdatedAt.Equal(r.Snapshot.UpdatedAt) {
		b, e := json.Marshal(&current)
		if e != nil {
			return false, e
		}
		if dispositionHash(string(b)) == r.Fingerprint {
			current.IsSynced = state == WriteBehindCompleted
			current.HandedOff = state == WriteBehindHandedOff
			if current.IsSynced {
				current.SyncedAt = time.Now().UTC()
			}
			if e = dispositionWrite(txn, r.Snapshot.Key, &current); e != nil {
				return false, e
			}
			_, e = txn.Get([]byte(p.syncQueueKey(r.Snapshot.Key)))
			removed = e == nil
			if e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
				return false, e
			}
			if e = txn.Delete([]byte(p.syncQueueKey(r.Snapshot.Key))); e != nil {
				return false, e
			}
		}
	}
	r.State = state
	r.LocalConfirmed = removed && state == WriteBehindCompleted
	r.HandlerDeadline = time.Time{}
	r.NextAttemptAt = time.Time{}
	return removed, p.saveDisposition(txn, r)
}

// ApplyWriteBehindDecision 由受信任应用恢复/处置原生命周期，不重置任何预算；Release仅解除已交接的顺序屏障。
func (p *PrefixedBadgerDB[T]) ApplyWriteBehindDecision(ctx context.Context, d WriteBehindDecision) error {
	if p.IsClosed() {
		return ErrWriteStoreClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.disposition == nil {
		return ErrWriteBehindDisposition
	}
	return p.applyDispositionDecision(ctx, d, 0, false)
}

// updateWriteBehindTransaction 只在显式处置模式串行本prefix的本地事务，防止组索引/序号读改写冲突。
// 不自动重放含外部副作用的闭包，不在target调用或应用失败回调期间持锁；旧模式原样委托Badger。
func (p *PrefixedBadgerDB[T]) updateWriteBehindTransaction(fn func(*badger.Txn) error) error {
	if p.disposition != nil {
		p.disposition.storageMu.Lock()
		defer p.disposition.storageMu.Unlock()
	}
	return p.manager.db.Update(fn)
}

// rebuildDispositionSyncQueue 分页修复原索引并在提交时复核当前版本，不复活已交接项，也不收集全部历史key到内存。
func (p *PrefixedBadgerDB[T]) rebuildDispositionSyncQueue() (int, error) {
	cursor := ""
	total := 0
	limit := p.manager.config.WriteBehindFailure.ScanLimit
	for {
		keys := []string{}
		err := p.manager.db.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			it := txn.NewIterator(opts)
			defer it.Close()
			seek := p.prefix + cursor
			it.Seek([]byte(seek))
			if cursor != "" && it.Valid() && string(it.Item().Key()) == seek {
				it.Next()
			}
			for ; it.ValidForPrefix([]byte(p.prefix)) && len(keys) < limit; it.Next() {
				keys = append(keys, string(it.Item().Key()))
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		if len(keys) == 0 {
			return total, nil
		}
		added := 0
		err = p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
			for _, key := range keys {
				var item SyncQueueItem[T]
				if e := dispositionRead(txn, key, &item); e != nil {
					return e
				}
				if item.IsSynced || item.HandedOff {
					continue
				}
				_, e := txn.Get([]byte(p.syncQueueKey(key)))
				if e == nil {
					continue
				}
				if !errors.Is(e, badger.ErrKeyNotFound) {
					return e
				}
				if e = txn.Set([]byte(p.syncQueueKey(key)), nil); e != nil {
					return e
				}
				added++
			}
			return nil
		})
		if err != nil {
			return total, err
		}
		total += added
		cursor = keys[len(keys)-1][len(p.prefix):]
	}
}
