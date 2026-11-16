// 本文件仅合并既有FIFO中相邻且同store的独立组确认命令，不跨前台请求、屏障或字节/条数预算。
package nosql

import (
	"context"
	"errors"
	"time"
)

func (b *BatchCommitter[T]) submitLocalConfirmation(command *writeBehindConfirmation[T]) error {
	result, err := b.queueLocalRequest(context.Background(), batchCommitRequest[T]{confirmation: command, queuedAt: time.Now(), result: make(chan batchCommitResponse, 1)})
	if err != nil {
		return err
	}
	return (<-result).err
}

func (b *BatchCommitter[T]) finishConfirmationBatch(first batchCommitRequest[T]) (pending *batchCommitRequest[T]) {
	batch := []batchCommitRequest[T]{first}
	owner := first.confirmation.store
	maxItems := dispositionConfirmationMaxItems
	if owner.localConfirmationLimit > 0 && owner.localConfirmationLimit < maxItems {
		maxItems = owner.localConfirmationLimit
	}
	size := first.confirmation.bytesBudget
	items := len(first.confirmation.selected)
collect:
	for items < maxItems && size < dispositionConfirmationMaxBytes {
		select {
		case next, ok := <-b.requests:
			if !ok {
				break collect
			}
			if next.confirmation == nil || next.confirmation.store != owner {
				pending = &next
				break collect
			}
			cost := next.confirmation.bytesBudget
			if items+len(next.confirmation.selected) > maxItems || size+cost > dispositionConfirmationMaxBytes {
				pending = &next
				break collect
			}
			batch = append(batch, next)
			items += len(next.confirmation.selected)
			size += cost
		default:
			break collect
		}
	}
	// 只执行本地原子写集；计数、hook及失败通知仍由等待真实结果的原worker在FIFO之外处理。
	commands := make([]*writeBehindConfirmation[T], 0, len(batch))
	started := time.Now()
	owner.syncMetricsMu.Lock()
	for _, r := range batch {
		owner.syncMetrics.ConfirmationLocal.QueueWait += started.Sub(r.queuedAt)
		commands = append(commands, r.confirmation)
	}
	owner.syncMetricsMu.Unlock()
	func() {
		defer func() {
			if recover() != nil {
				for _, c := range commands {
					if !c.finished {
						c.commitErr = errors.New("本地确认事务panic")
						c.finished = true
					}
				}
			}
		}()
		owner.commitConfirmationCommands(commands)
	}()
	for _, r := range batch {
		r.result <- batchCommitResponse{err: r.confirmation.commitErr}
	}
	return pending
}
