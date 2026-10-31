// 本文件把可选失败状态接入原store生命周期，使用固定回调worker而非每条goroutine。
package nosql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type writeBehindDispositionJob[T types.IModel] struct {
	record WriteBehindFailureRecord[T]
	cause  error
}
type writeBehindDispositionRuntime[T types.IModel] struct {
	activeIDs    sync.Map // 仅固定worker持有，至多HandlerWorkers项
	activeGroups sync.Map
	storageMu    sync.Mutex // 仅串行本prefix本地元数据提交，不跨target/处置回调持锁
	policy       WriteBehindFailurePolicy
	handler      WriteBehindFailureHandler[T]
	ctx          context.Context
	cancel       context.CancelFunc
	jobs         chan writeBehindDispositionJob[T]
	// groupCheckObserver 仅供包内诊断同步观察实际核验；准备阶段设置，不改变写集或记录业务身份。
	groupCheckObserver func(*badger.Txn, badger.IteratorOptions)
}

// UseWriteBehindWithFailureHandler 显式启用有限处置；必须在接受写入前绑定，需与持久policy指纹一致。
func (p *PrefixedBadgerDB[T]) UseWriteBehindWithFailureHandler(target WriteBehindTarget[T], handler WriteBehindFailureHandler[T]) error {
	policy := p.manager.config.WriteBehindFailure
	if policy == nil || policy.Validate() != nil || target == nil || handler == nil {
		return ErrWriteBehindDisposition
	}
	if (reflect.ValueOf(handler).Kind() == reflect.Ptr && reflect.ValueOf(handler).IsNil()) || (reflect.ValueOf(target).Kind() == reflect.Ptr && reflect.ValueOf(target).IsNil()) {
		return ErrWriteBehindDisposition
	}
	if err := p.validateWriteBehindConfig(); err != nil {
		return err
	}
	p.closeMu.RLock()
	defer p.closeMu.RUnlock()
	if p.closed {
		return ErrWriteStoreClosed
	}
	p.syncLock.Lock()
	if p.syncDB || p.disposition != nil {
		p.syncLock.Unlock()
		return ErrWriteBehindAlreadyBound
	}
	ctx, cancel := context.WithCancel(context.Background())
	rt := &writeBehindDispositionRuntime[T]{policy: *policy, handler: handler, ctx: ctx, cancel: cancel, jobs: make(chan writeBehindDispositionJob[T], policy.HandlerWorkers)}
	p.disposition = rt
	err := p.bootstrapDispositions()
	if err != nil {
		p.disposition = nil
		p.syncLock.Unlock()
		cancel()
		return err
	}
	p.syncTarget = target
	p.syncList = nil
	p.syncDB = true
	p.syncBindErr = nil
	p.syncLock.Unlock()
	for range rt.policy.HandlerWorkers {
		p.wg.Add(1)
		go p.dispositionHandlerWorker()
	}
	p.startWriteBehindWorker()
	return nil
}

func (p *PrefixedBadgerDB[T]) bootstrapDispositions() error {
	base := p.dispositionPrefix()
	b, _ := json.Marshal(p.disposition.policy)
	fingerprint := dispositionHash(string(b))
	if err := p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		var old string
		e := dispositionRead(txn, base+"mode", &old)
		if e == nil && old != fingerprint {
			return ErrWriteBehindDisposition
		}
		if e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
			return e
		}
		return dispositionWrite(txn, base+"mode", fingerprint)
	}); err != nil {
		return err
	}
	// 启动时分段补齐旧pending；每段有界，监听前完成，不在业务请求里扫描全部历史。
	for _, prefix := range []string{p.syncQueuePrefix(), base + "q:"} {
		cursor := ""
		for {
			keys := []string{}
			err := p.manager.db.View(func(txn *badger.Txn) error {
				opts := badger.DefaultIteratorOptions
				opts.PrefetchValues = false
				it := txn.NewIterator(opts)
				defer it.Close()
				seek := prefix + cursor
				it.Seek([]byte(seek))
				if cursor != "" && it.Valid() && string(it.Item().Key()) == seek {
					it.Next()
				}
				for ; it.ValidForPrefix([]byte(prefix)) && len(keys) < p.disposition.policy.ScanLimit; it.Next() {
					keys = append(keys, string(it.Item().Key()))
				}
				return nil
			})
			if err != nil {
				return err
			}
			if len(keys) == 0 {
				break
			}
			err = p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
				for _, key := range keys {
					if prefix == p.syncQueuePrefix() {
						dataKey := p.dataKeyFromSyncQueueKey(key)
						var existing string
						e := dispositionRead(txn, base+"key:"+dispositionHash(dataKey), &existing)
						imported := errors.Is(e, badger.ErrKeyNotFound)
						if e != nil && !imported {
							return e
						}
						if e = p.ensureDispositionEntry(txn, dataKey); e != nil {
							return e
						}
						if imported {
							if e = dispositionRead(txn, base+"key:"+dispositionHash(dataKey), &existing); e != nil {
								return e
							}
							var r WriteBehindFailureRecord[T]
							if e = dispositionRead(txn, base+"r:"+existing, &r); e != nil {
								return e
							}
							r.State = WriteBehindUnknown
							r.Reason = "legacy_outcome_unknown"
							r.FirstFailureAt = time.Now().UTC()
							if e = p.saveDisposition(txn, &r); e != nil {
								return e
							}
						}
						continue
					}
					var id string
					if e := dispositionRead(txn, key, &id); errors.Is(e, badger.ErrKeyNotFound) {
						continue
					} else if e != nil {
						return e
					}
					var r WriteBehindFailureRecord[T]
					if e := dispositionRead(txn, base+"r:"+id, &r); e != nil {
						return e
					}
					if r.State == WriteBehindInFlight || r.State == WriteBehindHandling {
						r.State = WriteBehindUnknown
						r.Reason = "recovered_unknown"
						if r.FirstFailureAt.IsZero() {
							r.FirstFailureAt = time.Now().UTC()
						}
						r.HandlerDeadline = time.Time{}
						if e := p.saveDisposition(txn, &r); e != nil {
							return e
						}
					}
				}
				return nil
			})
			if err != nil {
				return err
			}
			cursor = keys[len(keys)-1][len(prefix):]
		}
	}
	return nil
}

func (p *PrefixedBadgerDB[T]) dispositionBudgetExhausted(r *WriteBehindFailureRecord[T], now time.Time) bool {
	policy := p.disposition.policy
	return (policy.MaxAttempts > 0 && r.Attempts >= policy.MaxAttempts) || (policy.MaxElapsed > 0 && !r.FirstAttemptAt.IsZero() && now.Sub(r.FirstAttemptAt) >= policy.MaxElapsed)
}

func (p *PrefixedBadgerDB[T]) dispatchDisposition(r WriteBehindFailureRecord[T], cause error) {
	rt := p.disposition
	if p.IsClosed() {
		return
	}
	// 先预留有限队列空位；仅主worker/手动同步的串行round调用此函数。
	if _, active := rt.activeIDs.Load(r.ID); active {
		return
	}
	if _, active := rt.activeGroups.Load(dispositionGroup(&r)); active {
		return
	}
	if len(rt.jobs) == cap(rt.jobs) {
		return
	}
	now := time.Now().UTC()
	err := p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		if e := dispositionRead(txn, p.dispositionPrefix()+"r:"+r.ID, &r); e != nil {
			return e
		}
		if r.State != WriteBehindUnknown || r.NextAttemptAt.After(now) {
			return ErrWriteBehindDisposition
		}
		if r.HandlerAttempts >= rt.policy.HandlerMaxAttempts {
			r.State = WriteBehindPaused
			r.Reason = "handler_budget_exhausted"
			return p.saveDisposition(txn, &r)
		}
		r.State = WriteBehindHandling
		r.HandlerAttempts++
		r.HandlerDeadline = now.Add(rt.policy.HandlerTimeout)
		return p.saveDisposition(txn, &r)
	})
	if err != nil || r.State != WriteBehindHandling {
		return
	}
	select {
	case rt.jobs <- writeBehindDispositionJob[T]{r, cause}:
	case <-rt.ctx.Done():
	default:
		// 并发人工round使队列满时保守暂停；不会增开worker或重新执行target。
		_ = p.pauseDisposition(r.ID, r.HandlerAttempts, "handler_capacity")
	}
}

func (p *PrefixedBadgerDB[T]) pauseDisposition(id string, generation int, reason string) error {
	err := p.updateWriteBehindTransaction(func(txn *badger.Txn) error {
		var r WriteBehindFailureRecord[T]
		if e := dispositionRead(txn, p.dispositionPrefix()+"r:"+id, &r); e != nil {
			return e
		}
		if r.State != WriteBehindHandling || r.HandlerAttempts != generation {
			return ErrWriteBehindDisposition
		}
		r.State = WriteBehindPaused
		r.Reason = reason
		r.HandlerDeadline = time.Time{}
		return p.saveDisposition(txn, &r)
	})
	if err == nil {
		logx.Errorw("write_behind_disposition_paused", logx.Field("reason", reason))
	}
	return err
}

func (p *PrefixedBadgerDB[T]) dispositionHandlerWorker() {
	defer p.wg.Done()
	rt := p.disposition
	for {
		select {
		case <-rt.ctx.Done():
			return
		case job := <-rt.jobs:
			if rt.ctx.Err() != nil {
				return
			}
			ctx, cancel := context.WithDeadline(rt.ctx, job.record.HandlerDeadline)
			cause := job.cause
			if cause == nil {
				cause = fmt.Errorf("恢复失败说明: %s", job.record.ErrorMessage)
			}
			failure := WriteBehindFailure[T]{Record: job.record, Cause: cause, BudgetExhausted: p.dispositionBudgetExhausted(&job.record, time.Now()), Waited: time.Since(job.record.FirstFailureAt)}
			if job.record.BatchID != "" {
				var batch writeBehindFailureBatch
				e := p.manager.db.View(func(txn *badger.Txn) error {
					if e := dispositionRead(txn, p.dispositionPrefix()+"b:"+job.record.BatchID, &batch); e != nil {
						return e
					}
					// 同一快照核对各版本状态；崩溃在最终摘要前也不伪造确认集合。
					failure.Unconfirmed = nil
					failure.ConfirmedKeys = nil
					for _, ref := range batch.Selected {
						var current WriteBehindFailureRecord[T]
						if e := dispositionRead(txn, p.dispositionPrefix()+"r:"+ref.ID, &current); e != nil {
							return e
						}
						if current.LocalConfirmed {
							failure.ConfirmedKeys = append(failure.ConfirmedKeys, ref.Key)
						} else if !dispositionTerminal(current.State) {
							failure.Unconfirmed = append(failure.Unconfirmed, current)
						}
					}
					return nil
				})
				if e != nil {
					cancel()
					_ = p.pauseDisposition(job.record.ID, job.record.HandlerAttempts, "failure_snapshot_unavailable")
					continue
				}
				failure.BatchID = batch.ID
			}
			var d WriteBehindDecision
			var err error
			func() {
				if ctx.Err() != nil {
					err = ctx.Err()
					return
				}
				// 与调度/人工决策共用短本地互斥，消除“检查超时后、登记实际在途前”的重试窗口。
				rt.storageMu.Lock()
				claimErr := p.manager.db.View(func(txn *badger.Txn) error {
					var current WriteBehindFailureRecord[T]
					if e := dispositionRead(txn, p.dispositionPrefix()+"r:"+job.record.ID, &current); e != nil {
						return e
					}
					if current.State != WriteBehindHandling || current.HandlerAttempts != job.record.HandlerAttempts || !current.HandlerDeadline.After(time.Now()) {
						return ErrWriteBehindDisposition
					}
					rt.activeIDs.Store(job.record.ID, true)
					rt.activeGroups.Store(dispositionGroup(&job.record), true)
					return nil
				})
				rt.storageMu.Unlock()
				if claimErr != nil {
					err = claimErr
					return
				}
				defer func() { rt.activeIDs.Delete(job.record.ID); rt.activeGroups.Delete(dispositionGroup(&job.record)) }()
				defer func() {
					if recover() != nil {
						err = ErrWriteBehindDisposition
					}
				}()
				d, err = rt.handler.HandleFailure(ctx, failure)
			}()
			if err == nil {
				err = ctx.Err()
			}
			cancel()
			if p.IsClosed() {
				return
			}
			if d.ID != job.record.ID || !d.Version.Equal(job.record.Snapshot.UpdatedAt) || d.Fingerprint != job.record.Fingerprint {
				err = ErrWriteBehindDisposition
			}
			if err != nil {
				_ = p.pauseDisposition(job.record.ID, job.record.HandlerAttempts, "handler_failed")
			} else if err = p.applyDispositionDecision(context.Background(), d, job.record.HandlerAttempts, true); err != nil {
				_ = p.pauseDisposition(job.record.ID, job.record.HandlerAttempts, "invalid_decision")
			}
			p.triggerSync()
		}
	}
}

func (p *PrefixedBadgerDB[T]) syncWithDisposition() {
	rt := p.disposition
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		if rt.ctx.Err() != nil {
			return
		}
		confirmed, err := p.processDispositionRound(rt.ctx, p.manager.config.SyncBatchSize)
		delay := rt.policy.MinRetryInterval
		if confirmed > 0 {
			delay = p.manager.config.SyncBacklogDrainDelay
		}
		reason := "no_progress"
		if confirmed > 0 {
			reason = "progress"
		}
		logx.Debugw("write_behind_disposition_round", logx.Field("confirmed", confirmed), logx.Field("has_error", err != nil), logx.Field("scan_limit", rt.policy.ScanLimit), logx.Field("next_wait_ms", delay.Milliseconds()), logx.Field("reason", reason))
		if delay == 0 {
			continue
		}
		timer.Reset(delay)
		select {
		case <-rt.ctx.Done():
			return
		case <-p.syncTrigger:
		case <-timer.C:
		}
	}
}
