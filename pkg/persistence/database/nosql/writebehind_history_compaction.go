// 本文件通过既有worker和本地提交面有界回收终态技术历史；不删除业务模型或未解决责任。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

const terminalDispositionFormat = "terminal-history-v1:"
const terminalMetadataGrace = time.Minute

func (p *PrefixedBadgerDB[T]) terminalHistoryKey(r *WriteBehindFailureRecord[T]) string {
	return fmt.Sprintf("%sgc:%020d:%020d:%s", p.dispositionPrefix(), r.HistoryAt.UnixMilli(), r.Sequence, r.ID)
}

// terminalDispositionForStorage只瘦身持久副本；提交后的hook仍持有完整原确认快照。
func (p *PrefixedBadgerDB[T]) terminalDispositionForStorage(txn *badger.Txn, r *WriteBehindFailureRecord[T]) (*WriteBehindFailureRecord[T], error) {
	if !dispositionTerminal(r.State) {
		return r, nil
	}
	if r.Snapshot == nil {
		return nil, ErrWriteBehindDisposition
	}
	out := *r
	snapshot := *r.Snapshot
	out.Snapshot = &snapshot
	if out.TerminalAt.IsZero() {
		out.TerminalAt = time.Now().UTC()
	}
	base := p.dispositionPrefix()
	var old struct {
		AuditBytes int64
		TerminalAt time.Time
		HistoryAt  time.Time
	}
	e := dispositionRead(txn, base+"r:"+r.ID, &old)
	if e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
		return nil, e
	}
	if !old.TerminalAt.IsZero() && r.TerminalAt.IsZero() {
		out.TerminalAt = old.TerminalAt
	}
	if !old.HistoryAt.IsZero() {
		oldKey := p.terminalHistoryKey(&WriteBehindFailureRecord[T]{ID: r.ID, Sequence: r.Sequence, HistoryAt: old.HistoryAt})
		if e := txn.Delete([]byte(oldKey)); e != nil {
			return nil, e
		}
	}
	policy := p.manager.config.WriteBehindTerminalRetention
	if policy == nil && old.AuditBytes == 0 {
		out.AuditBytes = 0
		snapshot.Item = nil
		if out.HistoryAt.IsZero() {
			out.HistoryAt = out.TerminalAt.Add(terminalMetadataGrace)
		}
		if e := txn.Set([]byte(p.terminalHistoryKey(&out)), []byte(r.ID)); e != nil {
			return nil, e
		}
		return &out, nil
	}
	var usage [2]int64
	if e := dispositionRead(txn, base+"audit-budget", &usage); e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
		return nil, e
	}
	if old.AuditBytes > 0 {
		usage[0]--
		usage[1] -= old.AuditBytes
	}
	if usage[0] < 0 || usage[1] < 0 {
		return nil, ErrWriteBehindDisposition
	}
	out.AuditBytes = 0
	if policy != nil && snapshot.Item != nil && time.Now().Before(out.TerminalAt.Add(policy.MaxAge)) {
		raw, e := json.Marshal(&snapshot)
		if e != nil {
			return nil, e
		}
		cost := int64(len(raw))
		if usage[0] < policy.MaxRecords && cost <= policy.MaxBytes-usage[1] {
			out.AuditBytes = cost
			usage[0]++
			usage[1] += cost
		}
	}
	if out.AuditBytes == 0 {
		snapshot.Item = nil
	}
	if old.AuditBytes != out.AuditBytes {
		if e := dispositionWrite(txn, base+"audit-budget", usage); e != nil {
			return nil, e
		}
	}
	if out.HistoryAt.IsZero() {
		out.HistoryAt = out.TerminalAt.Add(terminalMetadataGrace)
		if out.AuditBytes > 0 && policy != nil && policy.MaxAge > terminalMetadataGrace {
			out.HistoryAt = out.TerminalAt.Add(policy.MaxAge)
		}
	}
	if e := txn.Set([]byte(p.terminalHistoryKey(&out)), []byte(r.ID)); e != nil {
		return nil, e
	}
	return &out, nil
}

// initializeTerminalHistory启动分页建立旧卷引用与回收索引；检查点同批提交，未完成不接收业务写。
func (p *PrefixedBadgerDB[T]) initializeTerminalHistory() error {
	base := p.dispositionPrefix()
	prefix := base + "r:"
	for {
		var cursor string
		if e := p.manager.db.View(func(txn *badger.Txn) error {
			e := dispositionRead(txn, base+"history-init", &cursor)
			if errors.Is(e, badger.ErrKeyNotFound) {
				return nil
			}
			return e
		}); e != nil {
			return e
		}
		if cursor == "done" {
			return p.initializeTerminalBatches()
		}
		var ids []string
		if e := p.manager.db.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.Prefix = []byte(prefix)
			it := txn.NewIterator(opts)
			defer it.Close()
			it.Seek([]byte(prefix + cursor))
			if cursor != "" && it.Valid() && string(it.Item().Key()) == prefix+cursor {
				it.Next()
			}
			for ; it.ValidForPrefix(opts.Prefix) && len(ids) < 8; it.Next() {
				ids = append(ids, string(it.Item().Key())[len(prefix):])
			}
			return nil
		}); e != nil {
			return e
		}
		if e := p.initializeTerminalRecordBlock(ids); e != nil {
			return e
		}
	}
}

// CompactWriteBehindHistory显式执行一页有界维护，进度持久保存；不触发业务删除hook或远端写。
func (p *PrefixedBadgerDB[T]) CompactWriteBehindHistory(ctx context.Context, limit int) (WriteBehindHistoryCompaction, error) {
	var out WriteBehindHistoryCompaction
	if p.IsClosed() {
		return out, ErrWriteStoreClosed
	}
	if p.disposition == nil {
		return out, ErrWriteBehindDisposition
	}
	if limit < 1 || limit > 1000 {
		return out, ErrInvalidSyncLimit
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if e := ctx.Err(); e != nil {
		return out, e
	}
	p.syncExecMu.Lock()
	defer p.syncExecMu.Unlock()
	prefix := p.dispositionPrefix() + "gc:"
	var keys []string
	now := time.Now().UnixMilli()
	if e := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		opts.Prefix = []byte(prefix)
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix) && len(keys) < limit; it.Next() {
			key := string(it.Item().Key())
			parts := strings.SplitN(strings.TrimPrefix(key, prefix), ":", 2)
			if len(parts) != 2 {
				return ErrWriteBehindDisposition
			}
			due, e := strconv.ParseInt(parts[0], 10, 64)
			if e != nil || due < 0 {
				return ErrWriteBehindDisposition
			}
			if due > now {
				break
			}
			keys = append(keys, key)
		}
		return nil
	}); e != nil {
		return out, e
	}
	if len(keys) == 0 {
		out.CycleComplete = true
		return out, nil
	}
	for start := 0; start < len(keys); {
		end := start + 8
		if end > len(keys) {
			end = len(keys)
		}
		n, e := p.compactTerminalBlock(ctx, keys[start:end])
		out.Scanned += n.Scanned
		out.Stripped += n.Stripped
		out.Removed += n.Removed
		out.Deferred += n.Deferred
		if e != nil {
			return out, e
		}
		start = end
	}
	return out, nil
}

// deferTerminalHistory原子移动既有终态回收索引，不追逐持续新增项，也不让受保护旧项占满窗口。
func (p *PrefixedBadgerDB[T]) deferTerminalHistory(txn *badger.Txn, key string, r *WriteBehindFailureRecord[T], due time.Time) error {
	if e := txn.Delete([]byte(key)); e != nil {
		return e
	}
	r.HistoryAt = due
	if e := dispositionWrite(txn, p.dispositionPrefix()+"r:"+r.ID, r); e != nil {
		return e
	}
	return txn.Set([]byte(p.terminalHistoryKey(r)), []byte(r.ID))
}

func (p *PrefixedBadgerDB[T]) compactTerminalBlock(ctx context.Context, keys []string) (WriteBehindHistoryCompaction, error) {
	var out WriteBehindHistoryCompaction
	base := p.dispositionPrefix()
	e := p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		for _, key := range keys {
			out.Scanned++
			item, e := txn.Get([]byte(key))
			if errors.Is(e, badger.ErrKeyNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			value, e := item.ValueCopy(nil)
			if e != nil {
				return e
			}
			var r WriteBehindFailureRecord[T]
			if e := dispositionRead(txn, base+"r:"+string(value), &r); e != nil {
				return e
			}
			if !dispositionTerminal(r.State) || r.Snapshot == nil || r.ID != string(value) || r.HistoryAt.IsZero() || key != p.terminalHistoryKey(&r) {
				return ErrWriteBehindDisposition
			}
			if _, active := p.disposition.activeIDs.Load(r.ID); active {
				if e := p.deferTerminalHistory(txn, key, &r, time.Now().Add(time.Second)); e != nil {
					return e
				}
				out.Deferred++
				continue
			}
			policy := p.manager.config.WriteBehindTerminalRetention
			if time.Now().Before(r.TerminalAt.Add(terminalMetadataGrace)) && r.Snapshot.Item == nil {
				if e := p.deferTerminalHistory(txn, key, &r, r.TerminalAt.Add(terminalMetadataGrace)); e != nil {
					return e
				}
				out.Deferred++
				continue
			}
			if r.AuditBytes > 0 && policy != nil && time.Now().Before(r.TerminalAt.Add(policy.MaxAge)) {
				if e := p.deferTerminalHistory(txn, key, &r, r.TerminalAt.Add(policy.MaxAge)); e != nil {
					return e
				}
				out.Deferred++
				continue
			}
			stored, e := p.terminalDispositionForStorage(txn, &r)
			if e != nil {
				return e
			}
			if r.Snapshot.Item != nil && stored.Snapshot.Item == nil {
				out.Stripped++
			}
			if e := dispositionWrite(txn, base+"r:"+r.ID, stored); e != nil {
				return e
			}
			if stored.AuditBytes > 0 || time.Now().Before(stored.TerminalAt.Add(terminalMetadataGrace)) {
				if e := p.deferTerminalHistory(txn, key, stored, time.Now().Add(time.Second)); e != nil {
					return e
				}
				out.Deferred++
				continue
			}
			r = *stored
			// 一个原版本可以参与多次有界尝试，必须保留所有仍被失败批次引用的状态。
			memberOpts := badger.DefaultIteratorOptions
			memberOpts.PrefetchValues = false
			memberOpts.Prefix = []byte(base + "member:" + r.ID + ":")
			memberIt := txn.NewIterator(memberOpts)
			var members []string
			for memberIt.Seek(memberOpts.Prefix); memberIt.ValidForPrefix(memberOpts.Prefix) && len(members) < 128; memberIt.Next() {
				members = append(members, string(memberIt.Item().Key()))
			}
			memberIt.Close()
			ready := true
			for _, member := range members {
				id := member[len(memberOpts.Prefix):]
				done, e := p.retireDispositionBatch(txn, id)
				if e != nil {
					return e
				}
				if !done {
					ready = false
					continue
				}
				var batch writeBehindFailureBatch
				if e := dispositionRead(txn, base+"b:"+id, &batch); e != nil {
					return e
				}
				if batch.Remaining <= 0 {
					return ErrWriteBehindDisposition
				}
				batch.Remaining--
				if batch.Remaining == 0 {
					e = txn.Delete([]byte(base + "b:" + id))
				} else {
					e = dispositionWrite(txn, base+"b:"+id, &batch)
				}
				if e != nil {
					return e
				}
				if e := txn.Delete([]byte(member)); e != nil {
					return e
				}
			}
			if len(members) == 128 {
				ready = false
			}
			if !ready {
				if e := p.deferTerminalHistory(txn, key, &r, time.Now().Add(time.Second)); e != nil {
					return e
				}
				out.Deferred++
				continue
			}
			// 将所有后继原子接回更老前序，绝不把superseded当作跳过未解决责任的凭证。
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.Prefix = []byte(base + "ref:" + r.ID + ":")
			it := txn.NewIterator(opts)
			var successors []string
			for it.Seek(opts.Prefix); it.ValidForPrefix(opts.Prefix); it.Next() {
				successors = append(successors, string(it.Item().Key()))
				if len(successors) == 128 {
					break
				}
			}
			it.Close()
			for _, ref := range successors {
				var next WriteBehindFailureRecord[T]
				if e := dispositionRead(txn, base+"r:"+ref[len(string(opts.Prefix)):], &next); e != nil {
					return e
				}
				if next.PreviousID != r.ID || next.Sequence <= r.Sequence || next.Snapshot == nil || next.Snapshot.Key != r.Snapshot.Key {
					return ErrWriteBehindDisposition
				}
				next.PreviousID = r.PreviousID
				if e := dispositionWrite(txn, base+"r:"+next.ID, &next); e != nil {
					return e
				}
				if next.PreviousID != "" {
					if e := txn.Set([]byte(base+"ref:"+next.PreviousID+":"+next.ID), nil); e != nil {
						return e
					}
				}
				if e := txn.Delete([]byte(ref)); e != nil {
					return e
				}
			}
			if len(successors) == 128 {
				if e := p.deferTerminalHistory(txn, key, &r, time.Now().Add(time.Second)); e != nil {
					return e
				}
				out.Deferred++
				continue
			}
			if r.PreviousID != "" {
				if e := txn.Delete([]byte(base + "ref:" + r.PreviousID + ":" + r.ID)); e != nil {
					return e
				}
			}
			mapKey := base + "key:" + dispositionHash(r.Snapshot.Key)
			var currentID string
			if e := dispositionRead(txn, mapKey, &currentID); e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
				return e
			}
			if currentID == r.ID {
				if e := txn.Delete([]byte(mapKey)); e != nil {
					return e
				}
			}
			if e := txn.Delete([]byte(base + "r:" + r.ID)); e != nil {
				return e
			}
			if e := txn.Delete([]byte(key)); e != nil {
				return e
			}
			out.Removed++
		}
		if observer := p.disposition.historyCommitObserver; observer != nil {
			return observer(txn)
		}
		return nil
	})
	if errors.Is(e, badger.ErrTxnTooBig) && len(keys) > 1 {
		n, a := p.compactTerminalBlock(ctx, keys[:len(keys)/2])
		m, b := p.compactTerminalBlock(ctx, keys[len(keys)/2:])
		n.Scanned += m.Scanned
		n.Stripped += m.Stripped
		n.Removed += m.Removed
		n.Deferred += m.Deferred
		return n, errors.Join(a, b)
	}
	if e != nil {
		return WriteBehindHistoryCompaction{}, e
	}
	return out, nil
}

// retireDispositionBatch只有所有原成员均终态且其回调真实退出才退休；每页最多128次成员核验。
func (p *PrefixedBadgerDB[T]) retireDispositionBatch(txn *badger.Txn, id string) (bool, error) {
	if id == "" {
		return true, nil
	}
	base := p.dispositionPrefix()
	var batch writeBehindFailureBatch
	if e := dispositionRead(txn, base+"b:"+id, &batch); e != nil {
		return false, e
	}
	if batch.ID != id {
		return false, ErrWriteBehindDisposition
	}
	if batch.Retired {
		if batch.Remaining <= 0 || len(batch.Selected) != 0 {
			return false, ErrWriteBehindDisposition
		}
		return true, nil
	}
	if batch.RetireCursor < 0 || batch.RetireCursor > len(batch.Selected) {
		return false, ErrWriteBehindDisposition
	}
	end := batch.RetireCursor + 128
	if end > len(batch.Selected) {
		end = len(batch.Selected)
	}
	for _, ref := range batch.Selected[batch.RetireCursor:end] {
		if _, active := p.disposition.activeIDs.Load(ref.ID); active {
			return false, nil
		}
		var r WriteBehindFailureRecord[T]
		if e := dispositionRead(txn, base+"r:"+ref.ID, &r); e != nil {
			return false, e
		}
		if !dispositionTerminal(r.State) {
			batch.RetireCursor = 0
			return false, dispositionWrite(txn, base+"b:"+id, &batch)
		}
	}
	batch.RetireCursor = end
	if end == len(batch.Selected) {
		batch.Retired = true
		batch.Remaining = len(batch.Selected)
		batch.Selected = nil
	}
	return batch.Retired, dispositionWrite(txn, base+"b:"+id, &batch)
}

// initializeTerminalBatches为旧卷每次尝试建立成员引用，完成前不允许历史删除；检查点可恢复。
func (p *PrefixedBadgerDB[T]) initializeTerminalBatches() error {
	base := p.dispositionPrefix()
	prefix := base + "b:"
	for {
		var cursor string
		var key string
		e := p.manager.db.View(func(txn *badger.Txn) error {
			e := dispositionRead(txn, base+"history-batches-init", &cursor)
			if e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
				return e
			}
			if cursor == "done" {
				return nil
			}
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = false
			opts.Prefix = []byte(prefix)
			it := txn.NewIterator(opts)
			defer it.Close()
			it.Seek([]byte(prefix + cursor))
			if cursor != "" && it.Valid() && string(it.Item().Key()) == prefix+cursor {
				it.Next()
			}
			if it.ValidForPrefix(opts.Prefix) {
				key = string(it.Item().Key())
			}
			return nil
		})
		if e != nil {
			return e
		}
		if cursor == "done" {
			return nil
		}
		e = p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
			if key == "" {
				return dispositionWrite(txn, base+"history-batches-init", "done")
			}
			var batch writeBehindFailureBatch
			if e := dispositionRead(txn, key, &batch); e != nil {
				return e
			}
			if batch.Retired {
				return ErrWriteBehindDisposition
			}
			var offset int
			if e := dispositionRead(txn, base+"history-batch-offset", &offset); e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
				return e
			}
			if offset < 0 || offset > len(batch.Selected) {
				return ErrWriteBehindDisposition
			}
			end := offset + 128
			if end > len(batch.Selected) {
				end = len(batch.Selected)
			}
			for _, ref := range batch.Selected[offset:end] {
				var r WriteBehindFailureRecord[T]
				if e := dispositionRead(txn, base+"r:"+ref.ID, &r); e != nil {
					return e
				}
				if r.Fingerprint != ref.Fingerprint || r.Snapshot == nil || !r.Snapshot.UpdatedAt.Equal(ref.Version) {
					return ErrWriteBehindDisposition
				}
				if e := txn.Set([]byte(base+"member:"+ref.ID+":"+batch.ID), nil); e != nil {
					return e
				}
			}
			if end < len(batch.Selected) {
				return dispositionWrite(txn, base+"history-batch-offset", end)
			}
			if e := txn.Delete([]byte(base + "history-batch-offset")); e != nil {
				return e
			}
			return dispositionWrite(txn, base+"history-batches-init", strings.TrimPrefix(key, prefix))
		})
		if e != nil {
			return e
		}
	}
}

func (p *PrefixedBadgerDB[T]) initializeTerminalRecordBlock(ids []string) error {
	base := p.dispositionPrefix()
	prefix := base + "r:"
	e := p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		groups := map[string]struct{}{}
		for _, id := range ids {
			var r WriteBehindFailureRecord[T]
			if e := dispositionRead(txn, prefix+id, &r); e != nil {
				return e
			}
			if r.Snapshot == nil || r.ID != id {
				return ErrWriteBehindDisposition
			}
			if r.PreviousID != "" {
				var prev WriteBehindFailureRecord[T]
				if e := dispositionRead(txn, prefix+r.PreviousID, &prev); e != nil {
					return e
				}
				if prev.Sequence >= r.Sequence || prev.Snapshot == nil || prev.Snapshot.Key != r.Snapshot.Key {
					return ErrWriteBehindDisposition
				}
				if e := txn.Set([]byte(base+"ref:"+r.PreviousID+":"+r.ID), nil); e != nil {
					return e
				}
			}
			if dispositionTerminal(r.State) {
				r.HistoryAt = time.Time{}
				// 旧格式没有完成时钟，不能声称它仍在显式审计保留窗口。
				if r.TerminalAt.IsZero() {
					r.TerminalAt = time.Unix(1, 0).UTC()
				}
				if e := p.saveDisposition(txn, &r, groups); e != nil {
					return e
				}
			}
		}
		if e := p.deleteEmptyDispositionGroups(txn, groups); e != nil {
			return e
		}
		if len(ids) == 0 {
			return dispositionWrite(txn, base+"history-init", "done")
		}
		return dispositionWrite(txn, base+"history-init", ids[len(ids)-1])
	})
	if errors.Is(e, badger.ErrTxnTooBig) && len(ids) > 1 {
		if e := p.initializeTerminalRecordBlock(ids[:len(ids)/2]); e != nil {
			return e
		}
		return p.initializeTerminalRecordBlock(ids[len(ids)/2:])
	}
	return e
}
