// 本文件校验应用原版本决策与持久接管回执，不把交接计作业务成功。
package nosql

import (
	"context"
	"github.com/zeromicro/go-zero/core/logx"
	"time"

	"github.com/dgraph-io/badger/v4"
)

func (p *PrefixedBadgerDB[T]) applyDispositionDecision(ctx context.Context, d WriteBehindDecision, generation int, callback bool) error {
	removed, applied := false, false
	var confirmedSnapshot *SyncQueueItem[T]
	var finalState WriteBehindFailureState
	var finalReason string
	err := p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		var r WriteBehindFailureRecord[T]
		if e := dispositionRead(txn, p.dispositionPrefix()+"r:"+d.ID, &r); e != nil {
			return e
		}
		defer func() { finalState = r.State; finalReason = r.Reason }()
		if d.ID == "" || d.Fingerprint != r.Fingerprint || !d.Version.Equal(r.Snapshot.UpdatedAt) {
			return ErrWriteBehindDisposition
		}
		if !callback {
			if r.State == WriteBehindHandedOff && d.Action == WriteBehindHandoff && r.Receipt != nil && d.Receipt != nil && *r.Receipt == *d.Receipt {
				return nil
			}
			if r.State == WriteBehindCompleted && d.Action == WriteBehindConfirm && d.Outcome == WriteBehindCommitted {
				return nil
			}
			if r.State == WriteBehindReleased && d.Action == WriteBehindRelease && d.Outcome == WriteBehindOrderingReleased {
				return nil
			}
		}
		now := time.Now().UTC()
		if callback && (r.State != WriteBehindHandling || r.HandlerAttempts != generation || !r.HandlerDeadline.After(now)) {
			return ErrWriteBehindDisposition
		}
		if !callback && r.State != WriteBehindPaused && r.State != WriteBehindHandedOff && r.State != WriteBehindUnknown {
			return ErrWriteBehindDisposition
		}
		if d.Scope != WriteBehindScopeKey && d.Scope != WriteBehindScopeGroup && d.Scope != WriteBehindScopeTarget {
			return ErrWriteBehindDisposition
		}
		if d.Scope == WriteBehindScopeGroup && r.Group == "" {
			return ErrWriteBehindDisposition
		}
		if r.State == WriteBehindHandedOff && d.Action != WriteBehindRelease {
			return ErrWriteBehindDisposition
		}
		r.Scope = d.Scope
		r.HandlerDeadline = time.Time{}
		switch d.Action {
		case WriteBehindRetry:
			if d.Outcome != WriteBehindNotCommitted || d.RetryAfter < p.disposition.policy.MinRetryInterval || d.RetryAfter > p.disposition.policy.MaxRetryInterval {
				return ErrWriteBehindDisposition
			}
			if p.dispositionBudgetExhausted(&r, now) {
				r.State = WriteBehindPaused
				r.Reason = "target_budget_exhausted"
				return p.saveDisposition(txn, &r)
			}
			r.State = WriteBehindRetrying
			r.Reason = "application_retry"
			r.NextAttemptAt = now.Add(d.RetryAfter)
		case WriteBehindResolve:
			if d.Outcome != WriteBehindStillUnknown {
				return ErrWriteBehindDisposition
			}
			r.State = WriteBehindUnknown
			r.Reason = "application_resolve"
			r.NextAttemptAt = now.Add(p.disposition.policy.MinRetryInterval)
			if r.HandlerAttempts >= p.disposition.policy.HandlerMaxAttempts {
				r.State = WriteBehindPaused
				r.Reason = "handler_budget_exhausted"
			}
		case WriteBehindPause:
			r.State = WriteBehindPaused
			r.Reason = "application_paused"
		case WriteBehindHandoff:
			receipt := d.Receipt
			if receipt == nil || receipt.DispositionID != r.ID || !receipt.Version.Equal(r.Snapshot.UpdatedAt) || receipt.Fingerprint != r.Fingerprint || receipt.ReceiptID == "" || receipt.Owner == "" || receipt.DurableReference == "" || len(receipt.DurableReference) > 4096 {
				return ErrWriteBehindDisposition
			}
			r.Receipt = receipt
			r.Reason = "durable_handoff"
			var e error
			removed, e = p.completeDisposition(txn, &r, WriteBehindHandedOff)
			return e
		case WriteBehindConfirm:
			if d.Outcome != WriteBehindCommitted {
				return ErrWriteBehindDisposition
			}
			r.Reason = "application_confirmed"
			var e error
			removed, e = p.completeDisposition(txn, &r, WriteBehindCompleted)
			applied = removed
			confirmedSnapshot = r.Snapshot
			return e
		case WriteBehindRelease:
			if r.State != WriteBehindHandedOff || d.Outcome != WriteBehindOrderingReleased {
				return ErrWriteBehindDisposition
			}
			r.State = WriteBehindReleased
			r.Reason = "application_released"
		default:
			return ErrWriteBehindDisposition
		}
		return p.saveDisposition(txn, &r)
	})
	if err == nil {
		switch finalState {
		case WriteBehindPaused:
			logx.Errorw("write_behind_disposition_paused", logx.Field("reason", finalReason))
		case WriteBehindHandedOff:
			logx.Infow("write_behind_disposition_handed_off", logx.Field("technical_only", true))
		case WriteBehindRetrying, WriteBehindUnknown:
			logx.Debugw("write_behind_disposition_scheduled", logx.Field("reason", finalReason))
		}
		if removed {
			p.incrementPendingCount(-1)
		}
		if applied {
			p.syncMetricsMu.Lock()
			p.syncMetrics.SyncedItems++
			p.syncMetricsMu.Unlock()
			// 接管、暂停及重复/旧版本决定没有实际成功确认，不能触发本地回收。
			p.scheduleSyncAfterDelete(p.collectSyncAfterDeleteCandidates([]*SyncQueueItem[T]{confirmedSnapshot}, []string{confirmedSnapshot.Key}))
		}
		p.triggerSync()
	}
	return err
}
