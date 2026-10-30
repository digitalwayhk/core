// 本文件按应用顺序组的最早未解决版本轮转选批；扫描和回调均有硬预算。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type writeBehindFailureIdentity struct {
	ID          string
	Key         string
	Version     time.Time
	Fingerprint string
	Group       string
}
type writeBehindFailureBatch struct {
	ID       string
	Selected []writeBehindFailureIdentity
}

func (p *PrefixedBadgerDB[T]) dispositionIdentity(r WriteBehindFailureRecord[T]) writeBehindFailureIdentity {
	return writeBehindFailureIdentity{ID: r.ID, Key: r.Snapshot.Key, Version: r.Snapshot.UpdatedAt, Fingerprint: r.Fingerprint, Group: r.Group}
}

func (p *PrefixedBadgerDB[T]) selectDispositionRound(limit int) (selected, notify []WriteBehindFailureRecord[T], err error) {
	base := p.dispositionPrefix()
	now := time.Now().UTC()
	err = p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		var targetID string
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		targets := txn.NewIterator(opts)
		targets.Seek([]byte(base + "target:"))
		if targets.ValidForPrefix([]byte(base + "target:")) {
			if e := targets.Item().Value(func(b []byte) error { return json.Unmarshal(b, &targetID) }); e != nil {
				targets.Close()
				return e
			}
		}
		targets.Close()
		var cursor string
		if e := dispositionRead(txn, base+"cursor", &cursor); e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
			return e
		}
		groups := txn.NewIterator(opts)
		defer groups.Close()
		prefix := base + "g:"
		groups.Seek([]byte(prefix + cursor))
		if cursor != "" && groups.Valid() && string(groups.Item().Key()) == prefix+cursor {
			groups.Next()
		}
		last := ""
		scanned := 0
		for ; groups.ValidForPrefix([]byte(prefix)) && scanned < p.disposition.policy.ScanLimit && len(selected) < limit; groups.Next() {
			scanned++
			group := string(groups.Item().Key())[len(prefix):]
			last = group
			queue := txn.NewIterator(opts)
			qprefix := base + "q:" + group + ":"
			queue.Seek([]byte(qprefix))
			if !queue.ValidForPrefix([]byte(qprefix)) {
				queue.Close()
				continue
			}
			var id string
			e := queue.Item().Value(func(b []byte) error { return json.Unmarshal(b, &id) })
			queue.Close()
			if e != nil {
				return e
			}
			var r WriteBehindFailureRecord[T]
			if e = dispositionRead(txn, base+"r:"+id, &r); e != nil {
				return e
			}
			if r.State == WriteBehindInFlight {
				r.State = WriteBehindUnknown
				r.Reason = "local_confirmation_unknown"
				if r.FirstFailureAt.IsZero() {
					r.FirstFailureAt = now
				}
				if e = p.saveDisposition(txn, &r); e != nil {
					return e
				}
			}
			if r.State == WriteBehindHandling && !r.HandlerDeadline.After(now) {
				r.State = WriteBehindPaused
				r.Reason = "handler_timeout"
				r.HandlerDeadline = time.Time{}
				if e = p.saveDisposition(txn, &r); e != nil {
					return e
				}
			}
			if _, active := p.disposition.activeGroups.Load(dispositionGroup(&r)); active {
				continue
			}
			if r.State == WriteBehindUnknown {
				notify = append(notify, r)
				continue
			}
			if (r.State != WriteBehindReady && r.State != WriteBehindRetrying) || r.NextAttemptAt.After(now) {
				continue
			}
			if targetID != "" && targetID != r.ID {
				continue
			}
			if r.PreviousID != "" {
				if _, active := p.disposition.activeIDs.Load(r.PreviousID); active {
					continue
				}
				var prev WriteBehindFailureRecord[T]
				if e = dispositionRead(txn, base+"r:"+r.PreviousID, &prev); e != nil {
					return e
				}
				if !dispositionTerminal(prev.State) {
					continue
				}
			}
			if p.dispositionBudgetExhausted(&r, now) {
				r.State = WriteBehindUnknown
				r.Reason = "target_budget_exhausted"
				if e = p.saveDisposition(txn, &r); e != nil {
					return e
				}
				notify = append(notify, r)
				continue
			}
			r.State = WriteBehindInFlight
			r.Attempts++
			if r.FirstAttemptAt.IsZero() {
				r.FirstAttemptAt = now
			}
			if e = p.saveDisposition(txn, &r); e != nil {
				return e
			}
			selected = append(selected, r)
		}
		if !groups.ValidForPrefix([]byte(prefix)) {
			last = ""
		}
		if len(selected) > 0 {
			batch := writeBehindFailureBatch{}
			for _, r := range selected {
				batch.Selected = append(batch.Selected, p.dispositionIdentity(r))
			}
			b, e := json.Marshal(batch.Selected)
			if e != nil {
				return e
			}
			batch.ID = dispositionHash(string(b) + ":" + strconv.Itoa(selected[0].Attempts))
			if e = dispositionWrite(txn, base+"b:"+batch.ID, batch); e != nil {
				return e
			}
			for i := range selected {
				selected[i].BatchID = batch.ID
				if e = p.saveDisposition(txn, &selected[i]); e != nil {
					return e
				}
			}
		}
		return dispositionWrite(txn, base+"cursor", last)
	})
	return
}

func (p *PrefixedBadgerDB[T]) processDispositionRound(ctx context.Context, limit int) (confirmed int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if limit <= 0 {
		return 0, ErrInvalidSyncLimit
	}
	if limit > p.manager.config.SyncBatchSize {
		limit = p.manager.config.SyncBatchSize
	}
	p.syncExecMu.Lock()
	started := time.Now()
	var notify []WriteBehindFailureRecord[T]
	var selected []WriteBehindFailureRecord[T]
	defer func() {
		p.syncExecMu.Unlock()
		p.recordSyncMetrics(confirmed, err, time.Since(started))
		for _, r := range notify {
			p.dispatchDisposition(r, err)
		}
	}()
	selected, notify, err = p.selectDispositionRound(limit)
	if err != nil || len(selected) == 0 {
		return
	}
	items := make([]*SyncQueueItem[T], 0, len(selected))
	allowed := map[string]bool{}
	for _, r := range selected {
		b, e := json.Marshal(r.Snapshot)
		if e != nil {
			return 0, e
		}
		var item SyncQueueItem[T]
		if e = json.Unmarshal(b, &item); e != nil {
			return 0, e
		}
		items = append(items, &item)
		allowed[item.Key] = true
	}
	keys := make([]string, 0, len(items))
	for _, v := range items {
		keys = append(keys, v.Key)
	}
	unlock := p.lockSyncKeys(keys)
	defer unlock()
	var result *WriteBehindResult
	targetStarted := time.Now()
	func() {
		defer func() {
			if recover() != nil {
				err = ErrWriteBehindDisposition
			}
		}()
		result, err = p.syncTarget.SyncBatch(ctx, items)
	}()
	p.syncMetricsMu.Lock()
	p.syncMetrics.TargetDuration += time.Since(targetStarted)
	p.syncMetricsMu.Unlock()
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
		result = nil
	}
	success := map[string]bool{}
	if result != nil {
		seen := map[string]int{}
		valid := true
		for kind, list := range [][]string{result.ConfirmedKeys, result.RetryKeys, result.DeadKeys} {
			for _, key := range list {
				if !allowed[key] || (seen[key] != 0 && seen[key] != kind+1) {
					valid = false
				}
				seen[key] = kind + 1
				if kind == 0 {
					success[key] = true
				}
			}
		}
		if !valid {
			success = map[string]bool{}
			err = errors.Join(err, ErrWriteBehindDisposition)
		}
	}
	n, notices, confirmationErr := p.commitDispositionResults(selected, success, err)
	confirmed += n
	notify = append(notify, notices...)
	err = errors.Join(err, confirmationErr)
	return
}
