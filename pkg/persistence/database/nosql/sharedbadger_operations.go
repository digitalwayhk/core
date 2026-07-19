// Package nosql 提供 PrefixedBadgerDB 有序可靠 Save/Delete 本地事务原语。
package nosql

import (
	"errors"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/json"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// WriteOperationType 表示可靠本地事务中的保存或删除操作。
type WriteOperationType uint8

const (
	// WriteOperationSave 表示按 key 执行可靠 upsert。
	WriteOperationSave WriteOperationType = iota + 1
	// WriteOperationDelete 表示按 key 写入可靠删除 tombstone。
	WriteOperationDelete
)

var (
	// ErrWriteConflictDeleted 表示普通 Save 试图复活已经写入 tombstone 的 key。
	ErrWriteConflictDeleted = errors.New("可靠写入不能复活已删除数据")
	// ErrInvalidWriteOperation 表示操作类型、item 或业务 key 无效。
	ErrInvalidWriteOperation = errors.New("可靠本地写操作无效")
)

const localWriteTransactionMaxOperations = 1000

// WriteOperation 描述一个按接收顺序提交的可靠本地 Save/Delete 操作。
type WriteOperation[T types.IModel] struct {
	Type WriteOperationType
	Item *T
}

// ApplyWriteOperations 在一个 Badger 事务内按切片顺序提交 Save/Delete 操作。
func (p *PrefixedBadgerDB[T]) ApplyWriteOperations(operations []WriteOperation[T]) (BatchWriteResult, error) {
	if p == nil || p.IsClosed() {
		return BatchWriteResult{}, fmt.Errorf("%w: BadgerDB 实例已关闭", ErrInvalidWriteOperation)
	}
	if err := p.writeBehindBindError(); err != nil {
		return BatchWriteResult{}, err
	}
	if len(operations) == 0 {
		return BatchWriteResult{}, nil
	}

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()
	committed := 0
	for start := 0; start < len(operations); start += localWriteTransactionMaxOperations {
		end := start + localWriteTransactionMaxOperations
		if end > len(operations) {
			end = len(operations)
		}
		batchCommitted, newQueueCount, err := p.applyWriteOperationBatchAdaptive(operations[start:end], needSync)
		committed += batchCommitted
		if needSync && newQueueCount > 0 {
			p.incrementPendingCount(newQueueCount)
		}
		if err != nil {
			if needSync && committed > 0 {
				p.triggerSync()
			}
			return BatchWriteResult{Committed: committed}, fmt.Errorf(
				"可靠本地批次提交失败（已成功 %d/%d）: %w",
				committed,
				len(operations),
				err,
			)
		}
	}
	if needSync {
		p.triggerSync()
	}
	return BatchWriteResult{Committed: committed}, nil
}

func (p *PrefixedBadgerDB[T]) applyWriteOperationBatchAdaptive(
	operations []WriteOperation[T],
	needSync bool,
) (committed int, newQueueCount int, err error) {
	newQueueCount, err = p.applyWriteOperationBatch(operations, needSync)
	if err == nil {
		return len(operations), newQueueCount, nil
	}
	if !errors.Is(err, badger.ErrTxnTooBig) || len(operations) <= 1 {
		return 0, 0, err
	}
	middle := len(operations) / 2
	leftCommitted, leftQueueCount, leftErr := p.applyWriteOperationBatchAdaptive(operations[:middle], needSync)
	if leftErr != nil {
		return leftCommitted, leftQueueCount, leftErr
	}
	rightCommitted, rightQueueCount, rightErr := p.applyWriteOperationBatchAdaptive(operations[middle:], needSync)
	return leftCommitted + rightCommitted, leftQueueCount + rightQueueCount, rightErr
}

func (p *PrefixedBadgerDB[T]) applyWriteOperationBatch(
	operations []WriteOperation[T],
	needSync bool,
) (int, error) {
	newQueueCount := 0
	err := p.manager.db.Update(func(txn *badger.Txn) error {
		wrappers := make(map[string]*SyncQueueItem[T], len(operations))
		loaded := make(map[string]struct{}, len(operations))
		queued := make(map[string]struct{}, len(operations))
		for index, operation := range operations {
			if operation.Item == nil {
				return fmt.Errorf("%w: 第 %d 项 item 不能为空", ErrInvalidWriteOperation, index)
			}
			key := p.generateKey(operation.Item)
			if key == "" {
				return fmt.Errorf("%w: 第 %d 项 key 不能为空", ErrInvalidWriteOperation, index)
			}
			if _, ok := loaded[key]; !ok {
				wrapper, err := readSyncQueueItem[T](txn, key)
				if err != nil {
					return err
				}
				wrappers[key] = wrapper
				loaded[key] = struct{}{}
			}

			wrapper, changed, err := applyWriteOperation(key, wrappers[key], operation)
			if err != nil {
				return fmt.Errorf("第 %d 项提交失败: %w", index, err)
			}
			wrappers[key] = wrapper
			if !changed {
				continue
			}
			data, err := json.Marshal(wrapper)
			if err != nil {
				return fmt.Errorf("序列化可靠写入失败 [key=%s]: %w", key, err)
			}
			if err := txn.Set([]byte(key), data); err != nil {
				return err
			}
			if !needSync {
				continue
			}
			if _, exists := queued[key]; exists {
				continue
			}
			created, err := p.ensureSyncQueueEntry(txn, key)
			if err != nil {
				return err
			}
			queued[key] = struct{}{}
			if created {
				newQueueCount++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newQueueCount, nil
}

func readSyncQueueItem[T types.IModel](txn *badger.Txn, key string) (*SyncQueueItem[T], error) {
	item, err := txn.Get([]byte(key))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var wrapper SyncQueueItem[T]
	if err := item.Value(func(value []byte) error { return json.Unmarshal(value, &wrapper) }); err != nil {
		return nil, err
	}
	return &wrapper, nil
}

func applyWriteOperation[T types.IModel](
	key string,
	wrapper *SyncQueueItem[T],
	operation WriteOperation[T],
) (*SyncQueueItem[T], bool, error) {
	switch operation.Type {
	case WriteOperationSave:
		return applySaveOperation(key, wrapper, operation.Item)
	case WriteOperationDelete:
		return applyDeleteOperation(wrapper)
	default:
		return wrapper, false, fmt.Errorf("%w: type=%d", ErrInvalidWriteOperation, operation.Type)
	}
}

func applySaveOperation[T types.IModel](key string, wrapper *SyncQueueItem[T], item *T) (*SyncQueueItem[T], bool, error) {
	now := time.Now()
	if wrapper != nil {
		if wrapper.IsDeleted {
			return wrapper, false, fmt.Errorf("%w [key=%s]", ErrWriteConflictDeleted, key)
		}
		wrapper.Op = OpUpdate
		wrapper.Item = item
		wrapper.UpdatedAt = now
		wrapper.IsSynced = false
		if rowDate, ok := any(item).(types.IRowDate); ok {
			rowDate.SetUpdatedAt(now)
			if rowDate.GetCreatedAt() == nil {
				rowDate.SetCreatedAt(wrapper.CreatedAt)
			}
		}
		return wrapper, true, nil
	}
	wrapper = &SyncQueueItem[T]{
		Key:       key,
		Item:      item,
		Op:        OpInsert,
		CreatedAt: now,
		UpdatedAt: now,
		IsSynced:  false,
		IsDeleted: false,
	}
	if rowDate, ok := any(item).(types.IRowDate); ok {
		rowDate.SetCreatedAt(now)
		rowDate.SetUpdatedAt(now)
	}
	return wrapper, true, nil
}

func applyDeleteOperation[T types.IModel](wrapper *SyncQueueItem[T]) (*SyncQueueItem[T], bool, error) {
	if wrapper == nil || wrapper.IsDeleted {
		return wrapper, false, nil
	}
	now := time.Now()
	wrapper.Op = OpDelete
	wrapper.IsDeleted = true
	wrapper.DeletedAt = now
	wrapper.UpdatedAt = now
	wrapper.IsSynced = false
	return wrapper, true, nil
}

// StorageSize 返回当前共享 Badger 的 LSM 与 value log 原生大小快照。
func (p *PrefixedBadgerDB[T]) StorageSize() BadgerSize {
	if p == nil || p.manager == nil {
		return BadgerSize{}
	}
	return p.manager.Size()
}
