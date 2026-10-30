// 本文件复用BatchCommitter现有FIFO，为Core本地元数据提交提供有限准入和真实结果等待。
package nosql

import (
	"context"
	"fmt"
)

func (b *BatchCommitter[T]) queueLocalCommit(ctx context.Context, commit func() error) (chan batchCommitResponse, error) {
	request := batchCommitRequest[T]{localCommit: commit, result: make(chan batchCommitResponse, 1)}
	b.stateMu.RLock()
	if b.localClosed {
		b.stateMu.RUnlock()
		return nil, ErrWriteStoreClosed
	}
	b.localSubmitters.Add(1)
	b.stateMu.RUnlock()
	defer b.localSubmitters.Done()
	select {
	case b.requests <- request:
		updateReliableAtomicMax(&b.metrics.maxQueueDepth, int64(len(b.requests)))
		return request.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.closing:
		return nil, ErrWriteStoreClosed
	}
}

func (b *BatchCommitter[T]) submitLocalCommit(commit func() error) error {
	result, err := b.queueLocalCommit(context.Background(), commit)
	if err != nil {
		return err
	}
	return (<-result).err // 接受后只认真实本地提交结果，不能用关闭或取消伪装成功。
}

// drainAcceptedWrites 停止前台准入并以同FIFO屏障等待已接收保存；内部确认仍可入队，防止Close自锁。
func (b *BatchCommitter[T]) drainAcceptedWrites(ctx context.Context) error {
	b.stateMu.Lock()
	b.closed = true
	b.stateMu.Unlock()
	b.submitters.Wait()
	result, err := b.queueLocalCommit(ctx, func() error { return nil })
	if err != nil {
		return err
	}
	select {
	case r := <-result:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *BatchCommitter[T]) finishLocalCommit(request batchCommitRequest[T]) {
	var err error
	func() {
		defer func() {
			if v := recover(); v != nil {
				err = fmt.Errorf("本地元数据提交panic: %v", v)
			}
		}()
		err = request.localCommit()
	}()
	request.result <- batchCommitResponse{err: err}
}
