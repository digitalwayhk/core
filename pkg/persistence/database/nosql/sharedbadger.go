package nosql

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/json"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// IMaxConcurrencyHint 由支持连接池的 DB 适配器实现，提示允许的最大并发数
type IMaxConcurrencyHint interface {
	GetMaxOpenConns() int
}

// adapterSemasMu 保护 adapterSemas 的并发访问
var adapterSemasMu sync.Mutex

// adapterSemas 以 adapter 指针值为键，持有各连接池的并发信号量。
// 作用域是 adapter 实例（连接池）而非 basePath：同一 adapter 的所有 prefix 共享同一信号量。
var adapterSemas = map[uintptr]chan struct{}{}

// getOrCreateAdapterSema 为 action 对应的连接池返回（或创建）专属信号量。
// 容量 = MaxOpenConns × 75%；若 adapter 未实现 IMaxConcurrencyHint 或 MaxOpenConns=0，默认 8。
func getOrCreateAdapterSema(action types.IDataAction) chan struct{} {
	v := reflect.ValueOf(action)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		// 非指针或空 adapter：退化为独立信号量
		return make(chan struct{}, 8)
	}
	ptr := v.Pointer()

	adapterSemasMu.Lock()
	defer adapterSemasMu.Unlock()
	if sema, ok := adapterSemas[ptr]; ok {
		return sema
	}
	concurrency := 8
	if hint, ok := action.(IMaxConcurrencyHint); ok {
		if n := hint.GetMaxOpenConns(); n > 0 {
			concurrency = n * 3 / 4
			if concurrency < 1 {
				concurrency = 1
			}
		}
	}
	sema := make(chan struct{}, concurrency)
	adapterSemas[ptr] = sema
	return sema
}

// ScanResult 分页扫描结果
type ScanResult[T types.IModel] struct {
	Items   []*T   `json:"items"`    // 数据列表
	LastKey string `json:"last_key"` // 最后一个 key（用于下次分页）
	HasMore bool   `json:"has_more"` // 是否还有更多数据
}
type OpType string

const (
	OpInsert OpType = "insert"
	OpUpdate OpType = "update"
	OpDelete OpType = "delete" // 🔧 删除操作
)

// SyncQueueItem 同步队列项（包装数据）
type SyncQueueItem[T types.IModel] struct {
	Key       string    `json:"key"`
	Item      *T        `json:"item,omitempty"`
	Op        OpType    `json:"op"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	IsSynced  bool      `json:"is_synced"`
	SyncedAt  time.Time `json:"synced_at,omitempty"`
	IsDeleted bool      `json:"is_deleted"`
	DeletedAt time.Time `json:"deleted_at,omitempty"`
}

// PrefixedBadgerDB 带前缀的共享 BadgerDB
type PrefixedBadgerDB[T types.IModel] struct {
	manager *SharedBadgerManager
	prefix  string // "user:", "order:", "product:"

	syncDB         bool
	syncList       *entity.ModelList[T]
	syncLock       sync.RWMutex
	syncMutex      sync.Mutex
	syncInProgress bool
	syncExecMu     sync.Mutex // 保证 processSyncQueue 串行执行，防止并发调用
	closeCh        chan struct{}
	syncTrigger    chan struct{} // 写入时立即触发同步，无需等待 ticker
	wg             sync.WaitGroup
	syncOnce       sync.Once
	isAutoClean    bool

	syncSema chan struct{} // 连接池并发信号量，与持有同一 adapter 的其他实例共享

	// 待同步计数缓存
	pendingCountCache int
	pendingCountMutex sync.RWMutex
	lastCountUpdate   time.Time

	// ✅ 关闭状态控制
	closeOnce sync.Once
	closed    bool
	closeMu   sync.RWMutex
}

// NewSharedBadgerDB 创建共享 BadgerDB 实例
func NewSharedBadgerDB[T types.IModel](basePath string, config ...BadgerDBConfig) (*PrefixedBadgerDB[T], error) {
	prefix := reflect.TypeOf((*T)(nil)).Elem().Name() + ":"
	manager, err := GetSharedManager(basePath, config...)
	if err != nil {
		return nil, err
	}

	manager.AddRef(prefix)

	db := &PrefixedBadgerDB[T]{
		manager:     manager,
		prefix:      prefix,
		closeCh:     make(chan struct{}),
		syncTrigger: make(chan struct{}, 1),
	}

	logx.Infof("共享BadgerDB实例已创建 [prefix=%s]", prefix)
	return db, nil
}

// generateKey 生成带前缀的 key
func (p *PrefixedBadgerDB[T]) generateKey(item *T) string {
	if item == nil {
		return ""
	}
	if rowCode, ok := any(item).(types.IRowCode); ok {
		return p.prefix + rowCode.GetHash()
	}
	return ""
}

// SetSyncDB 设置同步数据库
func (p *PrefixedBadgerDB[T]) SetSyncDB(list *entity.ModelList[T]) {
	p.syncLock.Lock()
	defer p.syncLock.Unlock()

	if list != nil {
		if p.syncDB {
			return
		}
		p.syncDB = true
	} else {
		if !p.syncDB {
			return
		}
		p.syncDB = false
	}

	p.syncList = list

	// 绑定连接池信号量：以 adapter 指针为键，同一连接池的所有 prefix 共用同一信号量。
	if list != nil {
		if action := list.GetAction(); action != nil {
			p.syncSema = getOrCreateAdapterSema(action)
			logx.Infof("同步信号量已绑定 [prefix=%s, cap=%d]", p.prefix, cap(p.syncSema))
		}
	}

	if list != nil && p.syncDB {
		p.syncOnce.Do(func() {
			p.wg.Add(1)
			go p.syncToOtherDB()
			logx.Infof("共享DB自动同步已启动 [prefix=%s]", p.prefix)
		})
	}
}

// Set 写入数据
func (p *PrefixedBadgerDB[T]) Set(item *T, ttl time.Duration, fn ...func(wrapper *SyncQueueItem[T])) error {
	if p.IsClosed() {
		return fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	key := p.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()

	data, err := p.setItem(key, needSync, item, fn...)
	if err != nil {
		return err
	}

	err = p.manager.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			entry = entry.WithTTL(ttl)
		}
		return txn.SetEntry(entry)
	})

	if err == nil && needSync {
		p.incrementPendingCount(1)
		p.triggerSync()
	}
	return err
}
func (p *PrefixedBadgerDB[T]) BatchInsert(items []*T) error {
	if p.IsClosed() {
		return fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	if len(items) == 0 {
		return nil
	}

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()

	// BadgerDB 单事务有内存上限（默认约 10MB），超过会触发 ErrTxnTooBig。
	// 按固定批次分组提交，每批最多 1000 条。
	const batchSize = 1000
	successCount := 0

	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]

		err := p.manager.db.Update(func(txn *badger.Txn) error {
			for _, item := range batch {
				key := p.generateKey(item)
				if key == "" {
					return badger.ErrEmptyKey
				}

				data, err := p.setItem(key, needSync, item)
				if err != nil {
					return err
				}

				entry := badger.NewEntry([]byte(key), data)
				if err := txn.SetEntry(entry); err != nil {
					return err
				}
			}
			return nil
		})

		if err != nil {
			return fmt.Errorf("BatchInsert 第 %d-%d 条失败（已成功 %d 条）: %w", start, end, successCount, err)
		}
		successCount += len(batch)
	}

	if needSync {
		p.incrementPendingCount(successCount)
		p.triggerSync()
	}
	return nil
}

// BatchLoad 将远端数据库拉取的数据批量写入本地 BadgerDB，标记为已同步（不会触发反向同步回远端）。
// 适用于用户登录时把远程数据拉到本地的场景。
func (p *PrefixedBadgerDB[T]) BatchLoad(items ...*T) error {
	if p.IsClosed() {
		return fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	if len(items) == 0 {
		return nil
	}

	const batchSize = 1000
	for start := 0; start < len(items); start += batchSize {
		end := start + batchSize
		if end > len(items) {
			end = len(items)
		}
		batch := items[start:end]

		err := p.manager.db.Update(func(txn *badger.Txn) error {
			now := time.Now()
			for _, item := range batch {
				key := p.generateKey(item)
				if key == "" {
					return badger.ErrEmptyKey
				}
				wrapper := &SyncQueueItem[T]{
					Key:       key,
					Item:      item,
					Op:        OpUpdate, // 远端数据直接写入本地，标记为更新（如果不存在则相当于插入）
					CreatedAt: now,
					UpdatedAt: now,
					IsSynced:  true, // 来自远端，无需再同步
					SyncedAt:  now,
				}
				data, err := json.Marshal(wrapper)
				if err != nil {
					return fmt.Errorf("序列化失败 [%s]: %w", key, err)
				}
				if err := txn.Set([]byte(key), data); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("BatchLoad 第 %d-%d 条失败: %w", start, end, err)
		}
	}
	return nil
}

// DeleteBySubPrefix 删除所有 key 以 subPrefix 开头的本地数据（物理删除，不触发同步）。
// 适用于用户退出时清理本地缓存。
// subPrefix 不需要带 p.prefix，方法内部自动拼接，例如传入 "102765296354105388888:" 即可。
//
// 行为：
//   - 已同步（IsSynced=true）的条目立即删除；
//   - 未同步（IsSynced=false）的条目跳过，由后台 goroutine 等待同步完成后自动删除；
//   - 返回值为本次立即删除的数量。
func (p *PrefixedBadgerDB[T]) DeleteBySubPrefix(subPrefix string) (int, error) {
	if p.IsClosed() {
		return 0, fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}

	fullPrefix := p.prefix + subPrefix

	// 扫描所有匹配的 key，按同步状态分组
	var syncedKeys, pendingKeys []string
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek([]byte(fullPrefix)); it.ValidForPrefix([]byte(fullPrefix)); it.Next() {
			key := string(it.Item().Key())
			synced := true // 反序列化失败时保守视为已同步（直接删除）
			_ = it.Item().Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err == nil && !wrapper.IsSynced {
					synced = false
				}
				return nil
			})
			if synced {
				syncedKeys = append(syncedKeys, key)
			} else {
				pendingKeys = append(pendingKeys, key)
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("扫描 key 失败 [prefix=%s]: %w", fullPrefix, err)
	}

	// 立即删除已同步的条目
	deleted := 0
	if len(syncedKeys) > 0 {
		deleted, err = p.physicalDeleteKeys(syncedKeys)
		if err != nil {
			return deleted, err
		}
	}

	// 对未同步的条目，后台等待同步完成后再删除
	if len(pendingKeys) > 0 {
		logx.Infof("DeleteBySubPrefix: 已删除 %d 条，%d 条待同步，后台自动清理中 [prefix=%s]",
			deleted, len(pendingKeys), fullPrefix)
		go p.waitAndDeleteKeys(pendingKeys, fullPrefix)
	}

	return deleted, nil
}

// waitAndDeleteKeys 后台轮询，等待指定 key 同步完成后物理删除。
func (p *PrefixedBadgerDB[T]) waitAndDeleteKeys(keys []string, logPrefix string) {
	const (
		pollInterval = 5 * time.Second
		maxWait      = 10 * time.Minute
	)
	deadline := time.Now().Add(maxWait)
	remaining := keys

	for len(remaining) > 0 {
		if time.Now().After(deadline) {
			logx.Errorf("DeleteBySubPrefix 后台清理超时，%d 条数据未能删除 [prefix=%s]", len(remaining), logPrefix)
			return
		}
		if p.IsClosed() {
			return
		}

		time.Sleep(pollInterval)

		// 重新检查哪些已同步完成
		var readyKeys, stillPending []string
		_ = p.manager.db.View(func(txn *badger.Txn) error {
			for _, k := range remaining {
				item, err := txn.Get([]byte(k))
				if err == badger.ErrKeyNotFound {
					continue // 已不存在，忽略
				}
				if err != nil {
					stillPending = append(stillPending, k)
					continue
				}
				synced := true
				_ = item.Value(func(val []byte) error {
					var wrapper SyncQueueItem[T]
					if err := json.Unmarshal(val, &wrapper); err == nil && !wrapper.IsSynced {
						synced = false
					}
					return nil
				})
				if synced {
					readyKeys = append(readyKeys, k)
				} else {
					stillPending = append(stillPending, k)
				}
			}
			return nil
		})

		if len(readyKeys) > 0 {
			n, err := p.physicalDeleteKeys(readyKeys)
			if err != nil {
				logx.Errorf("DeleteBySubPrefix 后台删除失败: %v [prefix=%s]", err, logPrefix)
			} else {
				logx.Infof("DeleteBySubPrefix 后台清理 %d 条 [prefix=%s]", n, logPrefix)
			}
		}
		remaining = stillPending
	}
	logx.Infof("DeleteBySubPrefix 后台清理完成 [prefix=%s]", logPrefix)
}

// physicalDeleteKeys 分批物理删除指定 key 列表（每批 1000 条）。
func (p *PrefixedBadgerDB[T]) physicalDeleteKeys(keys []string) (int, error) {
	const batchSize = 1000
	deleted := 0
	for start := 0; start < len(keys); start += batchSize {
		end := start + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[start:end]
		err := p.manager.db.Update(func(txn *badger.Txn) error {
			for _, k := range batch {
				if err := txn.Delete([]byte(k)); err != nil && err != badger.ErrKeyNotFound {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return deleted, fmt.Errorf("physicalDeleteKeys 第 %d-%d 条失败（已删 %d 条）: %w", start, end, deleted, err)
		}
		deleted += len(batch)
	}
	return deleted, nil
}

func (p *PrefixedBadgerDB[T]) setItem(key string, needSync bool, item *T, fn ...func(wrapper *SyncQueueItem[T])) ([]byte, error) {
	if item == nil {
		return nil, fmt.Errorf("item 不能为空")
	}

	existingWrapper, err := p.getWrapper(key)
	var wrapper *SyncQueueItem[T]

	if err == nil && existingWrapper != nil {
		if existingWrapper.IsDeleted {
			return nil, fmt.Errorf("无法更新已删除的项，key=%s", key)
		}
		wrapper = existingWrapper
		wrapper.Op = OpUpdate
		wrapper.Item = item
		wrapper.UpdatedAt = time.Now()
		wrapper.IsSynced = false
		if irde, ok := any(item).(types.IRowDate); ok {
			irde.SetUpdatedAt(wrapper.UpdatedAt)
			if irde.GetCreatedAt() == nil {
				irde.SetCreatedAt(wrapper.CreatedAt)
			}
		}
	} else {
		now := time.Now()
		wrapper = &SyncQueueItem[T]{
			Key:       key,
			Item:      item,
			Op:        OpInsert,
			CreatedAt: now,
			UpdatedAt: now,
			IsSynced:  false,
			IsDeleted: false,
		}
		if irde, ok := any(item).(types.IRowDate); ok {
			irde.SetCreatedAt(now)
			irde.SetUpdatedAt(now)
		}
	}

	if len(fn) > 0 {
		fn[0](wrapper)
	}

	data, err := json.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("序列化失败: %w", err)
	}
	return data, nil
}

// Get 获取数据
func (p *PrefixedBadgerDB[T]) Get(key string) (*T, error) {
	if p.IsClosed() {
		return nil, fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	fullKey := p.prefix + key
	wrapper, err := p.getWrapper(fullKey)
	if err != nil {
		return nil, err
	}

	if wrapper.IsDeleted {
		return nil, badger.ErrKeyNotFound
	}

	return wrapper.Item, nil
}

// getWrapper 内部方法
func (p *PrefixedBadgerDB[T]) getWrapper(key string) (*SyncQueueItem[T], error) {
	var wrapper = new(SyncQueueItem[T])

	err := p.manager.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, wrapper)
		})
	})

	if err != nil {
		return nil, err
	}

	if wrapper.Item != nil {
		if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
			hook.NewModel()
		}
	}

	return wrapper, nil
}

// Delete 删除数据
func (p *PrefixedBadgerDB[T]) Delete(key string) error {
	if p.IsClosed() {
		return fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	fullKey := p.prefix + key

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()

	return p.delete(fullKey, needSync)
}
func (p *PrefixedBadgerDB[T]) DeleteByItem(item *T) error {
	key := p.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()

	return p.delete(key, needSync)
}
func (p *PrefixedBadgerDB[T]) DeleteByItemWithSync(item *T, needSync bool) error {
	key := p.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	return p.delete(key, needSync)
}
func (p *PrefixedBadgerDB[T]) batchDelete(keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	return p.manager.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			if err := txn.Delete([]byte(key)); err != nil {
				return err
			}
		}
		return nil
	})
}

// delete 内部方法
func (p *PrefixedBadgerDB[T]) delete(key string, needSync bool) error {
	if !needSync {
		return p.manager.db.Update(func(txn *badger.Txn) error {
			return txn.Delete([]byte(key))
		})
	}

	if !p.syncDB {
		return fmt.Errorf("未启用同步数据库功能，无法执行软删除")
	}

	softDeleted := false
	err := p.manager.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}

		var wrapper SyncQueueItem[T]
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &wrapper)
		})
		if err != nil {
			return err
		}

		if wrapper.IsDeleted {
			return nil
		}

		now := time.Now()
		wrapper.Op = OpDelete
		wrapper.IsDeleted = true
		wrapper.DeletedAt = now
		wrapper.UpdatedAt = now
		wrapper.IsSynced = false

		data, err := json.Marshal(&wrapper)
		if err != nil {
			return fmt.Errorf("序列化失败: %w", err)
		}

		softDeleted = true
		return txn.Set([]byte(key), data)
	})
	if err == nil && softDeleted {
		p.incrementPendingCount(1)
		p.triggerSync()
	}
	return err
}

// Scan 扫描数据（仅扫描当前前缀）
func (p *PrefixedBadgerDB[T]) Scan(prefix string, limit int) ([]*T, error) {
	if p.IsClosed() {
		return nil, fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	var results []*T
	prefix = p.prefix + prefix
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 1000 // 增加预取大小
		opts.PrefetchValues = true
		opts.Reverse = false
		opts.AllVersions = false // 只读取最新版本
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			if limit > 0 && count >= limit {
				break
			}

			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				if wrapper.IsDeleted {
					return nil
				}

				if wrapper.Item != nil {
					if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
						hook.NewModel()
					}
					results = append(results, wrapper.Item)
					count++
				}
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}
		return nil
	})

	return results, err
}
func (p *PrefixedBadgerDB[T]) ScanAll() ([]*T, error) {
	var results []*T
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek([]byte(p.prefix)); it.ValidForPrefix([]byte(p.prefix)); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}
				if wrapper.IsDeleted {
					return nil
				}
				if wrapper.Item != nil {
					if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
						hook.NewModel()
					}
					results = append(results, wrapper.Item)
				}
				return nil
			})
			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}
		return nil
	})
	return results, err
}
func (p *PrefixedBadgerDB[T]) ScanPage(prefix string, limit int, lastKey string) (*ScanResult[T], error) {
	prefix = p.prefix + prefix
	if limit <= 0 {
		limit = 1000 // 默认每页 1000 条
	}

	result := &ScanResult[T]{
		Items: make([]*T, 0, limit),
	}

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		// 确定起始位置
		var startKey []byte
		if lastKey != "" {
			startKey = []byte(lastKey)
		} else {
			startKey = []byte(prefix)
		}

		count := 0
		firstItem := true

		for it.Seek(startKey); it.ValidForPrefix([]byte(prefix)); it.Next() {
			// 跳过上一页的最后一条（避免重复）
			if lastKey != "" && firstItem {
				currentKey := string(it.Item().Key())
				if currentKey == lastKey {
					firstItem = false
					continue
				}
			}
			firstItem = false

			// 达到限制后再读一条，判断是否还有更多数据
			if count >= limit {
				result.HasMore = true
				break
			}

			item := it.Item()
			currentKey := string(item.Key())

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 过滤已删除的数据
				if wrapper.IsDeleted {
					return nil
				}

				if wrapper.Item != nil {
					if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
						hook.NewModel()
					}
					result.Items = append(result.Items, wrapper.Item)
					result.LastKey = currentKey
					count++
				}
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}

		return nil
	})

	return result, err
}

// incrementPendingCount 更新待同步计数
func (p *PrefixedBadgerDB[T]) incrementPendingCount(delta int) {
	p.pendingCountMutex.Lock()
	p.pendingCountCache += delta
	p.pendingCountMutex.Unlock()
}

// triggerSync 通知 syncToOtherDB 立即执行同步（非阻塞，幂等）
func (p *PrefixedBadgerDB[T]) triggerSync() {
	select {
	case p.syncTrigger <- struct{}{}:
	default:
	}
}

// getDataAction 获取同步操作
func (p *PrefixedBadgerDB[T]) getDataAction(item *T) types.IDataAction {
	if p.syncList != nil {
		model := item
		if model == nil {
			model = new(T)
			if nm, ok := any(model).(types.IModelNewHook); ok {
				nm.NewModel()
			}
		}
		searchItem := p.syncList.GetSearchItem()
		searchItem.Model = model
		action := p.syncList.GetDBAdapter(searchItem)
		return action
	}
	return nil
}

// syncToOtherDB 同步到其他数据库（修复死循环问题 + 立即同步）
func (p *PrefixedBadgerDB[T]) syncToOtherDB() {
	defer p.wg.Done()

	config := p.manager.config
	interval := config.SyncInterval
	minInterval := config.SyncMinInterval
	maxInterval := config.SyncMaxInterval

	// 🆕 启动后立即检查一次
	p.syncLock.RLock()
	hasDB := p.syncDB
	p.syncLock.RUnlock()

	if hasDB {
		pendingCount, err := p.GetPendingSyncCount()
		if err == nil && pendingCount > 0 {
			logx.Infof("启动时发现待同步数据 [prefix=%s, count=%d], 立即执行同步", p.prefix, pendingCount)

			// 初始化 pendingCountCache，避免 ticker 因 cache=0 跳过同步
			p.pendingCountMutex.Lock()
			p.pendingCountCache = pendingCount
			p.pendingCountMutex.Unlock()

			p.syncMutex.Lock()
			p.syncInProgress = true
			p.syncMutex.Unlock()

			start := time.Now()
			if _, err := p.processSyncQueue(); err != nil {
			}
			duration := time.Since(start)

			p.pendingCountMutex.RLock()
			remainingCount := p.pendingCountCache
			p.pendingCountMutex.RUnlock()

			p.syncMutex.Lock()
			p.syncInProgress = false
			p.syncMutex.Unlock()

			logx.Infof("初始同步完成 [prefix=%s, 处理: %d, 剩余: %d, 耗时: %v]",
				p.prefix, pendingCount, remainingCount, duration)

			// 🆕 如果还有剩余数据,缩短间隔
			if remainingCount > 0 {
				interval = minInterval
			}
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.syncLock.RLock()
			hasDB := p.syncDB
			p.syncLock.RUnlock()

			if !hasDB {
				continue
			}

			// 用 pendingCountCache 替代全表扫描，避免大数据量下超时阻塞
			p.pendingCountMutex.RLock()
			pendingCount := p.pendingCountCache
			p.pendingCountMutex.RUnlock()

			if pendingCount <= 0 {
				interval = min(interval*2, maxInterval)
				ticker.Reset(interval)
				continue
			}

			p.syncMutex.Lock()
			if p.syncInProgress {
				p.syncMutex.Unlock()
				logx.Infof("上次同步未完成，跳过本次 [prefix=%s]", p.prefix)
				continue
			}
			p.syncInProgress = true
			p.syncMutex.Unlock()

			start := time.Now()

			synced, err := p.processSyncQueue()
			if err != nil {
				logx.Errorf("同步失败 [prefix=%s]: %v", p.prefix, err)
			}

			duration := time.Since(start)

			p.pendingCountMutex.RLock()
			remainingCount := p.pendingCountCache
			p.pendingCountMutex.RUnlock()

			p.syncMutex.Lock()
			p.syncInProgress = false
			p.syncMutex.Unlock()

			// 🔧 动态调整间隔
			if remainingCount > 0 {
				interval = minInterval
				if synced == 0 {
					// trigger case 已在处理这批数据，ticker 空转，不重复触发
					ticker.Reset(interval)
					continue
				}
				p.triggerSync() // 还有数据，立即再触发一次
			} else if duration < interval/2 {
				interval = max(interval/2, minInterval)
			} else if duration > interval {
				interval = min(duration*2, maxInterval)
			}

			ticker.Reset(interval)
			if synced > 0 {
				logx.Infof("同步完成 [prefix=%s, 同步: %d, 剩余: %d, 耗时: %v, 下次间隔: %v]",
					p.prefix, synced, remainingCount, duration, interval)
			}

		case <-p.syncTrigger:
			// 写入数据后立即触发同步（不受 ticker 间隔限制）
			p.syncLock.RLock()
			hasDB := p.syncDB
			p.syncLock.RUnlock()

			if !hasDB {
				continue
			}

			p.syncMutex.Lock()
			if p.syncInProgress {
				p.syncMutex.Unlock()
				continue
			}
			p.syncInProgress = true
			p.syncMutex.Unlock()

			// 短暂等待让窗口期内的小写入能合并到同一个 batch（减少小事务）
			if d := p.manager.config.SyncBatchDelay; d > 0 {
				time.Sleep(d)
			}

			synced, err := p.processSyncQueue()
			if err != nil {
				logx.Errorf("触发同步失败 [prefix=%s]: %v", p.prefix, err)
			}

			p.syncMutex.Lock()
			p.syncInProgress = false
			p.syncMutex.Unlock()

			p.pendingCountMutex.RLock()
			remaining := p.pendingCountCache
			p.pendingCountMutex.RUnlock()

			if synced > 0 {
				logx.Infof("触发同步完成 [prefix=%s, 同步: %d, 剩余: %d]", p.prefix, synced, remaining)
			}
			// 若还有数据，继续触发直到消费完
			if remaining > 0 && synced > 0 {
				p.triggerSync()
			}

		case <-p.closeCh:
			// ✅ 优化：添加超时保护，避免无限等待
			logx.Infof("收到关闭信号 [prefix=%s]", p.prefix)

			p.syncMutex.Lock()
			if p.syncInProgress {
				p.syncMutex.Unlock()

				// 等待同步完成，最多等待 10 秒
				logx.Infof("等待当前同步操作完成 [prefix=%s]", p.prefix)
				timeout := time.After(10 * time.Second)
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()

				for {
					select {
					case <-timeout:
						logx.Errorf("等待同步完成超时（10秒），强制退出 [prefix=%s]", p.prefix)
						return
					case <-ticker.C:
						p.syncMutex.Lock()
						if !p.syncInProgress {
							p.syncMutex.Unlock()
							logx.Infof("同步操作已完成 [prefix=%s]", p.prefix)
							logx.Infof("syncToOtherDB 退出 [prefix=%s]", p.prefix)
							return
						}
						p.syncMutex.Unlock()
					}
				}
			} else {
				p.syncMutex.Unlock()
			}

			logx.Infof("syncToOtherDB 退出 [prefix=%s]", p.prefix)
			return
		}
	}
}

// processSyncQueue 执行一轮同步，返回实际同步成功数和错误
func (p *PrefixedBadgerDB[T]) processSyncQueue() (int, error) {
	// 串行执行：如果有另一个调用正在进行，等其完成后再执行一次（不跳过）
	// 这样既防止并发事务冲突，又确保直接调用方（如测试）总能观察到最新的同步状态
	p.syncExecMu.Lock()
	defer p.syncExecMu.Unlock()

	unsyncedItems, err := p.getUnsyncedBatch(p.manager.config.SyncBatchSize)
	if err != nil {
		return 0, fmt.Errorf("获取未同步数据失败: %w", err)
	}

	if len(unsyncedItems) == 0 {
		return 0, nil
	}

	logx.Infof("开始同步 [prefix=%s, 数量: %d]", p.prefix, len(unsyncedItems))

	// 获取连接池信号量：限制同一 adapter 的并发 MySQL 事务数，防止打崩连接池
	if p.syncSema != nil {
		p.syncSema <- struct{}{}
		defer func() { <-p.syncSema }()
	}

	_, err = p.syncBatch(unsyncedItems)
	if err != nil {
		logx.Errorf("批量同步失败 [prefix=%s]: %v", p.prefix, err)
		return 0, err
	}
	successCount := 0
	for _, item := range unsyncedItems {
		if item.IsSynced {
			successCount++
		}
	}
	logx.Infof("同步成功 [prefix=%s, 成功: %d/%d]", p.prefix, successCount, len(unsyncedItems))
	return successCount, nil
}

// 🔧 修复 GetPendingSyncCount: 添加超时保护
func (p *PrefixedBadgerDB[T]) GetPendingSyncCount() (int, error) {
	count := 0

	// 🆕 添加超时保护
	done := make(chan error, 1)

	go func() {
		err := p.manager.db.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = true
			opts.PrefetchSize = 100 // 🔧 限制预取大小
			it := txn.NewIterator(opts)
			defer it.Close()

			for it.Seek([]byte(p.prefix)); it.ValidForPrefix([]byte(p.prefix)); it.Next() {
				item := it.Item()

				err := item.Value(func(val []byte) error {
					var wrapper SyncQueueItem[T]
					if err := json.Unmarshal(val, &wrapper); err != nil {
						return err
					}

					if !wrapper.IsSynced {
						count++
					}
					return nil
				})

				if err != nil {
					continue
				}
			}
			return nil
		})
		done <- err
	}()

	// 🆕 等待完成或超时
	select {
	case err := <-done:
		return count, err
	case <-time.After(5 * time.Second):
		return 0, fmt.Errorf("获取待同步数量超时 [prefix=%s]", p.prefix)
	}
}

// getUnsyncedBatch 获取未同步数据（限定前缀）
func (p *PrefixedBadgerDB[T]) getUnsyncedBatch(limit int) ([]*SyncQueueItem[T], error) {
	var items []*SyncQueueItem[T]

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Seek([]byte(p.prefix)); it.ValidForPrefix([]byte(p.prefix)); it.Next() {
			if count >= limit {
				break
			}

			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				if !wrapper.IsSynced {
					if wrapper.Item != nil {
						if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
							hook.NewModel()
						}
					}
					items = append(items, &wrapper)
					count++
				}
				return nil
			})

			if err != nil {
				continue
			}
		}
		return nil
	})

	return items, err
}

// 🔧 完整的批量同步实现（包含错误处理）
func (p *PrefixedBadgerDB[T]) syncBatch(items []*SyncQueueItem[T]) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}

	p.syncLock.RLock()
	if !p.syncDB {
		p.syncLock.RUnlock()
		return nil, fmt.Errorf("未开启 syncDB")
	}
	p.syncLock.RUnlock()

	// 按「操作类型 + 目标 DB 名」双维分组，确保每个事务只操作同一个库。
	// 若所有 item 的 GetRemoteDBName() 结果相同，行为等同于原来的单分组逻辑。
	type groupKey struct {
		op     OpType
		dbName string
	}
	groups := make(map[groupKey][]*SyncQueueItem[T])
	groupOrder := make([]groupKey, 0) // 保留插入顺序，使处理顺序可预测

	for _, wrapper := range items {
		setHashCode(wrapper.Item)

		dbName := ""
		if idb, ok := any(wrapper.Item).(types.IDBName); ok {
			dbName = idb.GetRemoteDBName()
			if dbName == "" {
				dbName = idb.GetLocalDBName()
			}
		}
		key := groupKey{op: wrapper.Op, dbName: dbName}
		if _, exists := groups[key]; !exists {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], wrapper)
	}

	successKeys := make([]string, 0, len(items))

	for _, key := range groupOrder {
		groupItems := groups[key]

		// 在 isTansaction=false 时完成路由：getDataAction 设置 searchItem.Model → GetDBName 读取
		// GetRemoteDBName() → 若库名变化则 m.db=nil → ensureValidConnection 建立新连接。
		// 之后传给批量函数，批量函数直接使用已路由的 action，无需再次路由。
		groupAction := p.getDataAction(groupItems[0].Item)
		if groupAction == nil {
			logx.Errorf("未找到同步操作对象 [db=%s, op=%s]", key.dbName, key.op)
			continue
		}
		if _, err := groupAction.GetModelDB(groupItems[0].Item); err != nil {
			logx.Errorf("路由目标 DB 失败 [db=%s, op=%s]: %v，降级为逐条处理", key.dbName, key.op, err)
			switch key.op {
			case OpInsert:
				successKeys = append(successKeys, p.insertItemsOneByOne(groupItems)...)
			case OpUpdate:
				successKeys = append(successKeys, p.updateItemsOneByOne(groupItems)...)
			case OpDelete:
				successKeys = append(successKeys, p.deleteItemsOneByOne(groupItems, groupAction)...)
			}
			p.onSyncAfter(groupItems)
			continue
		}

		switch key.op {
		case OpInsert:
			keys := p.batchInsertWithErrorHandling(groupItems, groupAction)
			successKeys = append(successKeys, keys...)
			p.onSyncAfter(groupItems)
		case OpUpdate:
			keys := p.batchUpdateWithErrorHandling(groupItems, groupAction)
			successKeys = append(successKeys, keys...)
			p.onSyncAfter(groupItems)
		case OpDelete:
			keys := p.batchDeleteWithErrorHandling(groupItems, groupAction)
			successKeys = append(successKeys, keys...)
			p.onSyncAfter(groupItems)
		}
	}

	// 批量更新同步状态
	if len(successKeys) > 0 {
		if err := p.batchUpdateSyncedStatus(successKeys); err != nil {
			logx.Errorf("更新同步状态失败（下次循环将重试这些记录）[prefix=%s]: %v", p.prefix, err)
		}
	}
	return successKeys, nil
}
func (p *PrefixedBadgerDB[T]) onSyncAfter(items []*SyncQueueItem[T]) {
	if len(items) == 0 {
		return
	}
	if iosa, ok := any(items[0].Item).(IOnSyncAfter[T]); ok {
		//如果异常不影响主流程，可以忽略错误
		// defer func() {
		// 	if err := recover(); err != nil {
		// 		logx.Errorf("执行 OnSyncAfter 发生恐慌: %v", err)
		// 	}
		// }()
		err := iosa.OnSyncAfter(items)
		if err != nil {
			logx.Errorf("执行 OnSyncAfter 失败: %v", err)
		}
	}
}
func setHashCode(item any) {
	if item == nil {
		return
	}
	if rowCode, ok := item.(types.IRowCode); ok {
		hash := rowCode.GetHash()
		if hash == "" {
			logx.Errorf("IRowCode GetHash 返回空字符串，可能导致 key 生成失败")
		}
		rowCode.SetHashcode(hash)
	}
}

// 🆕 批量插入（使用事务）
func (p *PrefixedBadgerDB[T]) batchInsertWithErrorHandling(items []*SyncQueueItem[T], syncAction types.IDataAction) []string {
	if len(items) == 0 {
		return nil
	}
	// 🆕 检查是否支持 Exists 方法
	type IExists interface {
		Exists(data interface{}) (bool, error)
	}
	successKeys := make([]string, 0, len(items))
	physicalDeleteKeys := make([]string, 0, len(items))

	// 🔧 开启事务（批量操作）
	if err := syncAction.Transaction(); err != nil {
		logx.Errorf("开启事务失败: %v，降级为逐条插入", err)
		return p.insertItemsOneByOne(items)
	}

	// 在事务中逐条插入
	hasError := false
	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}
		// 🆕 先检查数据是否存在（如果支持）
		shouldUpdate := false
		if existsChecker, ok := syncAction.(IExists); ok {
			exists, err := existsChecker.Exists(wrapper.Item)
			if err == nil && exists {
				shouldUpdate = true
				logx.Infof("数据已存在，直接更新 [%s]", wrapper.Key)
			}
		}
		var err error
		if shouldUpdate {
			// 直接更新
			err = syncAction.Update(wrapper.Item)
		} else {
			// 尝试插入
			err = syncAction.Insert(wrapper.Item)
		}
		if err != nil {
			// 🔧 处理主键/唯一索引冲突 - 尝试更新
			if strings.Contains(err.Error(), "Duplicate entry") ||
				strings.Contains(err.Error(), "Error 1062") ||
				strings.Contains(err.Error(), "duplicate key") ||
				strings.Contains(err.Error(), "UNIQUE constraint failed") {

				logx.Infof("插入冲突（唯一索引），尝试更新 [%s]: %v", wrapper.Key, err)

				// 🆕 尝试更新
				updateErr := syncAction.Update(wrapper.Item)
				if updateErr == nil {
					wrapper.IsSynced = true

					// 🔧 修复：更新成功后也要检查是否物理删除
					if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
						if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
							logx.Infof("ISyncAfterDelete 返回 true，将物理删除 [%s]", wrapper.Key)
							physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
							continue // 不加入 successKeys
						}
					}

					successKeys = append(successKeys, wrapper.Key)
					logx.Infof("✅ 插入冲突，更新成功 [%s]", wrapper.Key)
					continue
				}

				// 🆕 更新也失败，详细记录错误
				logx.Errorf("插入冲突后更新失败 [%s]: 插入错误=%v, 更新错误=%v", wrapper.Key, err, updateErr)
				hasError = true
				continue
			}

			// 其他错误
			logx.Errorf("插入失败 [%s]: %v", wrapper.Key, err)
			hasError = true
			continue
		}

		wrapper.IsSynced = true

		// 🔧 插入成功，检查是否需要物理删除
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				logx.Infof("ISyncAfterDelete 返回 true，将物理删除 [%s]", wrapper.Key)
				physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
				continue // 🔧 不加入 successKeys
			}
		}

		// 插入成功且不需要物理删除
		successKeys = append(successKeys, wrapper.Key)
	}

	// 🔧 提交事务
	if err := syncAction.Commit(); err != nil {
		logx.Errorf("提交事务失败: %v，回滚并降级为逐条处理", err)

		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}

		// 🆕 降级为逐条处理（会重试插入->更新）
		successKeys = p.insertItemsOneByOne(items)
	}

	// 🆕 批量物理删除本地缓存
	if len(physicalDeleteKeys) > 0 {
		if err := p.batchDelete(physicalDeleteKeys); err != nil {
			logx.Errorf("批量物理删除本地缓存失败: %v", err)
		}
	}

	if hasError {
		logx.Errorf("批量插入部分失败，成功: %d, 物理删除: %d, 总数: %d",
			len(successKeys), len(physicalDeleteKeys), len(items))
	} else {
		logx.Infof("✅ 批量插入完成，成功: %d, 物理删除: %d, 总数: %d",
			len(successKeys), len(physicalDeleteKeys), len(items))
	}

	// 🔧 减少待同步计数
	count := len(successKeys) + len(physicalDeleteKeys)
	p.incrementPendingCount(-count)

	return successKeys // 🔧 只返回需要更新同步状态的 keys
}

// 🆕 带超时的批量更新
func (p *PrefixedBadgerDB[T]) batchUpdateWithErrorHandling(items []*SyncQueueItem[T], syncAction types.IDataAction) []string {
	if len(items) == 0 {
		return nil
	}
	// 🆕 检查是否支持 Exists 方法
	type IExists interface {
		Exists(data interface{}) (bool, error)
	}
	successKeys := make([]string, 0, len(items))
	physicalDeleteKeys := make([]string, 0, len(items))

	// 🆕 添加超时保护
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 🔧 开启事务
	if err := syncAction.Transaction(); err != nil {
		logx.Errorf("开启事务失败: %v，降级为逐条更新", err)
		return p.updateItemsOneByOne(items)
	}

	// 🆕 确保事务一定被提交或回滚
	committed := false
	defer func() {
		if !committed {
			logx.Errorf("检测到未提交的事务，自动回滚")
			syncAction.Rollback()
		}
	}()

	// 🆕 使用通道接收结果
	done := make(chan struct{})
	hasError := false

	go func() {
		// 在事务中逐条更新
		for _, wrapper := range items {
			select {
			case <-ctx.Done():
				logx.Errorf("更新超时，停止批量更新")
				hasError = true
				return
			default:
			}

			if wrapper.Item == nil {
				continue
			}
			// 🆕 先检查数据是否存在（如果支持）
			shouldInsert := true
			if existsChecker, ok := syncAction.(IExists); ok {
				exists, err := existsChecker.Exists(wrapper.Item)
				if err == nil && exists {
					shouldInsert = false
					logx.Infof("数据已存在，直接更新 [%s]", wrapper.Key)
				}
			}
			var err error
			if shouldInsert {
				// 直接更新
				err = syncAction.Insert(wrapper.Item)
			} else {
				// 尝试插入
				err = syncAction.Update(wrapper.Item)
			}

			if err != nil {
				// 🔧 检查事务错误
				if strings.Contains(err.Error(), "transaction has already been committed") ||
					strings.Contains(err.Error(), "transaction has already been rolled back") {
					logx.Errorf("事务已失效，停止批量更新 [%s]", wrapper.Key)
					hasError = true
					return
				}

				// 🔧 检查连接错误
				if strings.Contains(err.Error(), "Rows are closed") ||
					strings.Contains(err.Error(), "context canceled") {
					logx.Errorf("连接已关闭，停止批量更新 [%s]", wrapper.Key)
					hasError = true
					return
				}

				// 🔧 处理记录不存在
				if strings.Contains(err.Error(), "record not found") ||
					strings.Contains(err.Error(), "no rows") {
					logx.Infof("记录不存在，尝试插入 [%s]", wrapper.Key)

					err = syncAction.Insert(wrapper.Item)
					if err == nil {
						if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
							if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
								physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
								continue
							}
						}
						successKeys = append(successKeys, wrapper.Key)
						continue
					}

					// 插入也失败
					if strings.Contains(err.Error(), "duplicate key") ||
						strings.Contains(err.Error(), "UNIQUE constraint failed") {
						logx.Errorf("插入冲突，重试更新 [%s]", wrapper.Key)
						err = syncAction.Update(wrapper.Item)
						if err == nil {
							if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
								if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
									physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
									continue
								}
							}
							successKeys = append(successKeys, wrapper.Key)
							continue
						}
					}

					logx.Errorf("插入失败 [%s]: %v", wrapper.Key, err)
					hasError = true
					continue
				}

				logx.Errorf("更新失败 [%s]: %v", wrapper.Key, err)
				hasError = true
				continue
			}

			wrapper.IsSynced = true

			// 更新成功，检查是否需要物理删除
			if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
				if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
					logx.Infof("ISyncAfterDelete 返回 true，将物理删除 [%s]", wrapper.Key)
					physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
					continue
				}
			}

			successKeys = append(successKeys, wrapper.Key)
		}
		close(done)
	}()

	// 🆕 等待完成或超时
	select {
	case <-done:
		// 正常完成
	case <-ctx.Done():
		logx.Errorf("批量更新超时 [数量: %d]", len(items))
		hasError = true
	}

	// 🔧 提交或回滚事务
	if hasError {
		logx.Errorf("批量更新遇到错误，回滚事务")
		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}
		committed = true
		return p.updateItemsOneByOne(items)
	}

	if err := syncAction.Commit(); err != nil {
		logx.Errorf("提交事务失败: %v", err)
		committed = true

		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}

		return p.updateItemsOneByOne(items)
	}

	committed = true

	// 🆕 批量物理删除
	if len(physicalDeleteKeys) > 0 {
		if err := p.batchDelete(physicalDeleteKeys); err != nil {
			logx.Errorf("批量物理删除失败: %v", err)
		}
	}

	if hasError {
		logx.Errorf("批量更新部分失败，成功: %d, 物理删除: %d, 总数: %d",
			len(successKeys), len(physicalDeleteKeys), len(items))
	}

	count := len(successKeys) + len(physicalDeleteKeys)
	p.incrementPendingCount(-count)

	return successKeys
}

// 🆕 批量删除（使用事务）- 增强版
func (p *PrefixedBadgerDB[T]) batchDeleteWithErrorHandling(items []*SyncQueueItem[T], syncAction types.IDataAction) []string {
	if len(items) == 0 {
		return nil
	}

	// 🆕 检查是否支持 Exists 方法
	type IExists interface {
		Exists(data interface{}) (bool, error)
	}

	successKeys := make([]string, 0, len(items))

	// 🔧 开启事务（批量操作）
	if err := syncAction.Transaction(); err != nil {
		logx.Errorf("开启事务失败: %v，降级为逐条删除", err)
		newSyncAction := p.getDataAction(items[0].Item)
		return p.deleteItemsOneByOne(items, newSyncAction)
	}

	// 在事务中逐条删除
	hasError := false
	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}

		// 🆕 删除前检查数据是否存在
		if existsChecker, ok := syncAction.(IExists); ok {
			exists, err := existsChecker.Exists(wrapper.Item)
			if err == nil && !exists {
				logx.Infof("数据不存在，跳过删除 [%s]", wrapper.Key)
				successKeys = append(successKeys, wrapper.Key)
				continue
			}

			// 🔧 如果数据存在，打印详细信息
			if exists {
				logx.Infof("🔍 准备删除数据 [%s]: %+v", wrapper.Key, wrapper.Item)
			}
		}

		err := syncAction.Delete(wrapper.Item)

		if err == nil {
			// 事务内 Delete 返回 nil 即视为成功，不在未提交事务内做 Exists 验证
			// （Exists 走独立连接，看不到未提交删除，会产生误报导致无限重试）
			successKeys = append(successKeys, wrapper.Key)
			wrapper.IsSynced = true
		}

		if err != nil {
			// 🔧 处理记录不存在 - 视为成功
			if strings.Contains(err.Error(), "record not found") ||
				strings.Contains(err.Error(), "no rows") {
				logx.Infof("删除目标不存在，跳过 [%s]", wrapper.Key)
				successKeys = append(successKeys, wrapper.Key)
				continue
			}

			logx.Errorf("删除失败 [%s]: %v", wrapper.Key, err)
			hasError = true
			continue
		}
	}

	// 🔧 提交事务
	if err := syncAction.Commit(); err != nil {
		logx.Errorf("提交事务失败: %v，回滚并降级为逐条处理", err)

		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}

		// 🆕 重新获取新的 syncAction
		newSyncAction := p.getDataAction(items[0].Item)
		if newSyncAction == nil {
			logx.Errorf("重新获取 syncAction 失败")
			return nil
		}

		successKeys = p.deleteItemsOneByOne(items, newSyncAction)
	} else {
		// Commit 成功后，用独立连接做一次真正的验证。
		// 若数据仍存在（可能是并发重新写入或 Delete 条件有误），
		// 将该 key 从 successKeys 中移除，使其保留在 BadgerDB 队列中，下次同步重试。
		if existsChecker, ok := syncAction.(IExists); ok {
			// 构建需要移除的 key 集合
			retryKeys := make(map[string]struct{})
			for _, wrapper := range items {
				if wrapper.Item == nil || !wrapper.IsSynced {
					continue
				}
				if stillExists, checkErr := existsChecker.Exists(wrapper.Item); checkErr == nil && stillExists {
					logx.Errorf("⚠️ Commit 后数据仍存在，保留队列下次重试 [%s]", wrapper.Key)
					retryKeys[wrapper.Key] = struct{}{}
					wrapper.IsSynced = false
				}
			}
			// 从 successKeys 中剔除需要重试的 key
			if len(retryKeys) > 0 {
				filtered := successKeys[:0]
				for _, k := range successKeys {
					if _, shouldRetry := retryKeys[k]; !shouldRetry {
						filtered = append(filtered, k)
					}
				}
				successKeys = filtered
				hasError = true
			}
		}
	}

	// 物理删除本地缓存
	for _, key := range successKeys {
		if err := p.delete(key, false); err != nil {
			logx.Errorf("物理删除本地缓存失败 [%s]: %v", key, err)
		}
	}

	if hasError {
		logx.Errorf("批量删除部分失败，成功: %d/%d", len(successKeys), len(items))
	} else {
		logx.Infof("✅ 批量删除完成，成功: %d/%d", len(successKeys), len(items))
	}

	count := len(successKeys)
	p.incrementPendingCount(-count)
	return successKeys
}

// 🆕 逐条插入（无事务）- 增强错误处理
func (p *PrefixedBadgerDB[T]) insertItemsOneByOne(items []*SyncQueueItem[T]) []string {
	successKeys := make([]string, 0, len(items))
	// 🆕 检查是否支持 Exists 方法
	type IExists interface {
		Exists(data interface{}) (bool, error)
	}
	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}

		wrapper.IsSynced = false
		syncAction := p.getDataAction(wrapper.Item)

		// 🆕 先检查数据是否存在（如果支持）
		shouldUpdate := false
		if existsChecker, ok := syncAction.(IExists); ok {
			exists, err := existsChecker.Exists(wrapper.Item)
			if err == nil && exists {
				shouldUpdate = true
				logx.Infof("数据已存在，直接更新 [%s]", wrapper.Key)
			}
		}
		var err error
		if shouldUpdate {
			// 直接更新
			err = syncAction.Update(wrapper.Item)
		} else {
			// 尝试插入
			err = syncAction.Insert(wrapper.Item)
		}
		if err != nil {
			// 🔧 处理主键/唯一索引冲突 - 尝试更新
			if strings.Contains(err.Error(), "Duplicate entry") ||
				strings.Contains(err.Error(), "Error 1062") ||
				strings.Contains(err.Error(), "duplicate key") ||
				strings.Contains(err.Error(), "UNIQUE constraint failed") {

				logx.Infof("插入冲突（唯一索引），尝试更新 [%s]: %v", wrapper.Key, err)

				updateErr := syncAction.Update(wrapper.Item)
				if updateErr == nil {
					wrapper.IsSynced = true

					// 检查是否需要物理删除
					if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
						if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
							if deleteErr := p.delete(wrapper.Key, false); deleteErr != nil {
								logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, deleteErr)
							}
							continue // 不加入 successKeys
						}
					}

					successKeys = append(successKeys, wrapper.Key)
					logx.Infof("✅ 插入冲突，更新成功 [%s]", wrapper.Key)
					continue
				}

				logx.Errorf("插入冲突后更新失败 [%s]: 插入错误=%v, 更新错误=%v", wrapper.Key, err, updateErr)
				continue
			}

			logx.Errorf("插入失败 [%s]: %v", wrapper.Key, err)
			continue
		}

		wrapper.IsSynced = true

		// 插入成功，检查是否需要物理删除
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				if deleteErr := p.delete(wrapper.Key, false); deleteErr != nil {
					logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, deleteErr)
				}
				continue
			}
		}

		successKeys = append(successKeys, wrapper.Key)
	}

	logx.Infof("✅ 逐条插入完成，成功: %d/%d", len(successKeys), len(items))
	return successKeys
}

// 🆕 逐条更新（无事务）
func (p *PrefixedBadgerDB[T]) updateItemsOneByOne(items []*SyncQueueItem[T]) []string {
	successKeys := make([]string, 0, len(items))
	// 🆕 检查是否支持 Exists 方法
	type IExists interface {
		Exists(data interface{}) (bool, error)
	}
	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}
		wrapper.IsSynced = false
		syncAction := p.getDataAction(wrapper.Item)

		// 🆕 先检查数据是否存在（如果支持）
		shouldUpdate := false
		if existsChecker, ok := syncAction.(IExists); ok {
			exists, err := existsChecker.Exists(wrapper.Item)
			if err == nil && exists {
				shouldUpdate = true
				logx.Infof("数据已存在，直接更新 [%s]", wrapper.Key)
			}
		}

		var err error
		if shouldUpdate {
			// 直接更新
			err = syncAction.Update(wrapper.Item)
		} else {
			// 尝试插入
			err = syncAction.Insert(wrapper.Item)
		}
		if err != nil {
			// 🔧 处理记录不存在 - 尝试插入
			if strings.Contains(err.Error(), "record not found") ||
				strings.Contains(err.Error(), "no rows") {
				logx.Infof("记录不存在，尝试插入 [%s]", wrapper.Key)

				err = syncAction.Insert(wrapper.Item)
				if err == nil {
					successKeys = append(successKeys, wrapper.Key)
					continue
				}

				// 插入也失败（可能是主键冲突，再尝试更新）
				if strings.Contains(err.Error(), "duplicate key") ||
					strings.Contains(err.Error(), "UNIQUE constraint failed") {
					logx.Errorf("插入冲突，重试更新 [%s]", wrapper.Key)
					err = syncAction.Update(wrapper.Item)
					if err == nil {
						successKeys = append(successKeys, wrapper.Key)
						continue
					}
				}

				logx.Errorf("插入失败 [%s]: %v", wrapper.Key, err)
				continue
			}

			logx.Errorf("更新失败 [%s]: %v", wrapper.Key, err)
			continue
		}
		wrapper.IsSynced = true
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				err := p.delete(wrapper.Key, false)
				if err != nil {
					logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err)
				}
				continue
			}
		}
		successKeys = append(successKeys, wrapper.Key)

	}

	return successKeys
}

// 🆕 逐条删除（无事务）
func (p *PrefixedBadgerDB[T]) deleteItemsOneByOne(items []*SyncQueueItem[T], syncAction types.IDataAction) []string {
	successKeys := make([]string, 0, len(items))
	// 🆕 检查是否支持 Exists 方法
	type IExists interface {
		Exists(data interface{}) (bool, error)
	}
	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}
		if existsChecker, ok := syncAction.(IExists); ok {
			exists, err := existsChecker.Exists(wrapper.Item)
			if err == nil && !exists {
				logx.Infof("数据不存在，跳过删除 [%s]", wrapper.Key)
				successKeys = append(successKeys, wrapper.Key)
				continue
			}
		}
		wrapper.IsSynced = false
		err := syncAction.Delete(wrapper.Item)

		if err != nil {
			// 🔧 处理记录不存在 - 视为成功
			if strings.Contains(err.Error(), "record not found") ||
				strings.Contains(err.Error(), "no rows") {
				logx.Infof("删除目标不存在，跳过 [%s]", wrapper.Key)
				successKeys = append(successKeys, wrapper.Key)

				// 物理删除本地缓存
				if err1 := p.delete(wrapper.Key, false); err1 != nil {
					logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err1)
				}
				continue
			}

			// 🔧 处理 WHERE 条件缺失 - 这是编程错误
			if strings.Contains(err.Error(), "WHERE conditions required") {
				logx.Errorf("删除条件缺失 [%s]，需要检查 Delete 实现: %v", wrapper.Key, err)
				// 不加入成功列表，等待重试
				continue
			}

			logx.Errorf("删除失败 [%s]: %v", wrapper.Key, err)
			continue
		}

		// 删除成功
		successKeys = append(successKeys, wrapper.Key)
		wrapper.IsSynced = true
		// 物理删除本地缓存
		if err1 := p.delete(wrapper.Key, false); err1 != nil {
			logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err1)
		}
	}

	return successKeys
}

// 🆕 批量更新同步状态（CAS 模式，避免覆盖）
func (p *PrefixedBadgerDB[T]) batchUpdateSyncedStatus(keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	now := time.Now()

	// ErrConflict 时最多重试 3 次（业务层并发写同一 key 时需要重试）
	const maxRetries = 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := p.manager.db.Update(func(txn *badger.Txn) error {
			for _, key := range keys {
				item, err := txn.Get([]byte(key))
				if err != nil {
					if err == badger.ErrKeyNotFound {
						continue
					}
					logx.Errorf("获取key失败 [%s]: %v", key, err)
					continue
				}

				var wrapper SyncQueueItem[T]
				if err = item.Value(func(val []byte) error {
					return json.Unmarshal(val, &wrapper)
				}); err != nil {
					logx.Errorf("反序列化失败 [%s]: %v", key, err)
					continue
				}

				// 已同步，跳过
				if wrapper.IsSynced {
					continue
				}

				// 有新写入（业务层在同步期间更新了数据），保留 IsSynced=false 让下次循环重新同步
				if attempt > 0 && !wrapper.UpdatedAt.IsZero() && wrapper.UpdatedAt.After(now) {
					continue
				}

				wrapper.IsSynced = true
				wrapper.SyncedAt = now

				data, err := json.Marshal(&wrapper)
				if err != nil {
					logx.Errorf("序列化失败 [%s]: %v", key, err)
					continue
				}

				if err := txn.Set([]byte(key), data); err != nil {
					return err // 把错误传播出去触发整体回滚
				}
			}
			return nil
		})

		if err == nil {
			return nil
		}

		// 并发冲突：重试
		if err == badger.ErrConflict && attempt < maxRetries-1 {
			logx.Infof("⚠️ 更新同步状态冲突，重试 [%d/%d] prefix=%s", attempt+1, maxRetries, p.prefix)
			time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
			continue
		}

		return err
	}
	return nil
}

func (p *PrefixedBadgerDB[T]) Count() (int, error) {
	count := 0

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(p.prefix)); it.ValidForPrefix([]byte(p.prefix)); it.Next() {
			count++
		}
		return nil
	})

	return count, err
}
func (p *PrefixedBadgerDB[T]) CountByPrefix(subPrefix string) (int, error) {
	count := 0
	fullPrefix := p.prefix + subPrefix

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true // 🔧 需要读取值来判断是否删除
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(fullPrefix)); it.ValidForPrefix([]byte(fullPrefix)); it.Next() {
			item := it.Item()

			// 🆕 解析数据，检查是否已删除
			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 只统计未删除的数据
				if !wrapper.IsDeleted {
					count++
				}
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}
		return nil
	})

	return count, err
}

// Close 关闭实例
func (p *PrefixedBadgerDB[T]) Close() error {
	return p.CloseWithTimeout(30*time.Second, 10*time.Second)
}

// CloseWithTimeout 带超时的关闭实例
func (p *PrefixedBadgerDB[T]) CloseWithTimeout(waitTimeout, syncTimeout time.Duration) error {
	// ✅ 使用 sync.Once 确保只关闭一次
	p.closeOnce.Do(func() {
		// 标记为已关闭
		p.closeMu.Lock()
		p.closed = true
		p.closeMu.Unlock()

		// 关闭 channel，通知 goroutine 退出
		close(p.closeCh)

		// ✅ 等待 goroutine 退出（带超时）
		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			logx.Infof("后台同步 goroutine 已退出 [prefix=%s]", p.prefix)
		case <-time.After(waitTimeout):
			logx.Errorf("等待后台 goroutine 退出超时（%v），强制关闭 [prefix=%s]", waitTimeout, p.prefix)

			// ✅ 检查是否还在同步中
			p.syncMutex.Lock()
			if p.syncInProgress {
				logx.Errorf("检测到正在进行的同步操作（等待最多 %v）[prefix=%s]", syncTimeout, p.prefix)

				// 等待同步完成或超时
				syncDone := make(chan struct{})
				go func() {
					for p.syncInProgress {
						time.Sleep(100 * time.Millisecond)
					}
					close(syncDone)
				}()

				select {
				case <-syncDone:
					logx.Infof("同步操作已完成 [prefix=%s]", p.prefix)
				case <-time.After(syncTimeout):
					logx.Errorf("等待同步完成超时（%v），强制退出 [prefix=%s]", syncTimeout, p.prefix)
				}
			}
			p.syncMutex.Unlock()
		}

		// 移除管理器引用
		p.manager.RemoveRef(p.prefix)

		logx.Infof("共享BadgerDB实例已关闭 [prefix=%s]", p.prefix)
	})

	return nil
}

// IsClosed 检查实例是否已关闭
func (p *PrefixedBadgerDB[T]) IsClosed() bool {
	p.closeMu.RLock()
	defer p.closeMu.RUnlock()
	return p.closed
}

// 🆕 CheckAndEnforceLimit 检查并执行数量限制
func (p *PrefixedBadgerDB[T]) CheckAndEnforceLimit(model *T) error {
	// 检查模型是否实现了 IAutoLimit 接口

	limitConfig, ok := any(model).(IAutoLimit[T])
	if !ok {
		return nil // 未实现接口,不执行限制
	}

	filterPrefix, maxCount, sortField, descending := limitConfig.GetLimitConfig()
	if maxCount <= 0 {
		return nil // 无限制
	}

	// 统计当前数量
	currentCount, err := p.CountByPrefix(filterPrefix)
	if err != nil {
		return fmt.Errorf("统计数量失败: %w", err)
	}

	if currentCount <= maxCount {
		return nil // 未超过限制
	}

	// 需要删除的数量
	deleteCount := currentCount - maxCount
	logx.Infof("数据超限 [prefix=%s, current=%d, max=%d, delete=%d]",
		p.prefix+filterPrefix, currentCount, maxCount, deleteCount)

	// 获取需要删除的旧数据
	keysToDelete, err := p.getOldestKeys(filterPrefix, deleteCount, sortField, descending)
	if err != nil {
		return fmt.Errorf("获取旧数据失败: %w", err)
	}

	if len(keysToDelete) == 0 {
		return nil
	}

	// 批量删除
	if err := p.batchDelete(keysToDelete); err != nil {
		return fmt.Errorf("批量删除失败: %w", err)
	}

	logx.Infof("自动清理完成 [prefix=%s, deleted=%d]", p.prefix+filterPrefix, len(keysToDelete))
	return nil
}

// 🆕 获取最旧的数据key列表
func (p *PrefixedBadgerDB[T]) getOldestKeys(filterPrefix string, count int, sortField string, descending bool) ([]string, error) {
	type itemWithKey struct {
		key       string
		sortValue interface{}
		timestamp time.Time
	}

	fullPrefix := p.prefix + filterPrefix
	items := make([]itemWithKey, 0)

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(fullPrefix)); it.ValidForPrefix([]byte(fullPrefix)); it.Next() {
			item := it.Item()
			key := string(item.Key())

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 跳过已删除的数据
				if wrapper.IsDeleted {
					return nil
				}

				// 提取排序字段值
				var sortValue interface{}
				var timestamp time.Time

				if wrapper.Item != nil {
					// 使用反射获取排序字段
					v := reflect.ValueOf(wrapper.Item)
					if v.Kind() == reflect.Ptr {
						v = v.Elem()
					}

					if v.Kind() == reflect.Struct {
						field := v.FieldByName(sortField)
						if field.IsValid() {
							sortValue = field.Interface()
							// 如果是时间类型
							if t, ok := sortValue.(time.Time); ok {
								timestamp = t
							}
						}
					}
				}

				// 如果没有找到排序字段,使用创建时间
				if timestamp.IsZero() {
					timestamp = wrapper.CreatedAt
				}

				items = append(items, itemWithKey{
					key:       key,
					sortValue: sortValue,
					timestamp: timestamp,
				})
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// 排序(根据时间戳)
	if descending {
		// 降序: 保留最新的,删除最旧的
		for i := 0; i < len(items); i++ {
			for j := i + 1; j < len(items); j++ {
				if items[i].timestamp.Before(items[j].timestamp) {
					items[i], items[j] = items[j], items[i]
				}
			}
		}
	} else {
		// 升序: 保留最旧的,删除最新的
		for i := 0; i < len(items); i++ {
			for j := i + 1; j < len(items); j++ {
				if items[i].timestamp.After(items[j].timestamp) {
					items[i], items[j] = items[j], items[i]
				}
			}
		}
	}

	// 取需要删除的key
	deleteCount := count
	if deleteCount > len(items) {
		deleteCount = len(items)
	}

	keys := make([]string, deleteCount)
	for i := 0; i < deleteCount; i++ {
		keys[i] = items[len(items)-deleteCount+i].key
	}

	return keys, nil
}

// 🆕 在 Set 方法后自动检查限制
func (p *PrefixedBadgerDB[T]) SetWithAutoLimit(item *T, ttl time.Duration, fn ...func(wrapper *SyncQueueItem[T])) error {
	err := p.Set(item, ttl, fn...)
	if err != nil {
		return err
	}

	// 异步检查并执行限制
	go func() {
		if err := p.CheckAndEnforceLimit(item); err != nil {
			logx.Errorf("自动限制检查失败: %v", err)
		}
	}()

	return nil
}

// ForceSyncAll 强制同步所有待同步数据（阻塞式）
func (p *PrefixedBadgerDB[T]) ForceSyncAll() error {
	p.closeMu.RLock()
	if p.closed {
		p.closeMu.RUnlock()
		return fmt.Errorf("BadgerDB 实例已关闭")
	}
	p.closeMu.RUnlock()

	if !p.syncDB || p.syncList == nil {
		return fmt.Errorf("同步功能未启用")
	}

	logx.Info("🔄 开始强制同步所有待同步数据...")

	totalSynced := 0
	maxIterations := 100 // 防止无限循环

	for i := 0; i < maxIterations; i++ {
		// 获取待同步数量
		pendingCount, err := p.GetPendingSyncCount()
		if err != nil {
			logx.Errorf("获取待同步数量失败: %v", err)
			return err
		}

		if pendingCount == 0 {
			logx.Infof("✅ 强制同步完成，共同步 %d 条数据", totalSynced)
			return nil
		}

		logx.Infof("📊 剩余待同步: %d 条（第 %d 次迭代）", pendingCount, i+1)

		syncedInThisBatch, err := p.processSyncQueue()
		if err != nil {
			logx.Errorf("强制同步失败: %v", err)
			return err
		}

		if syncedInThisBatch <= 0 {
			logx.Errorf("⚠️ 同步未取得进展，退出循环（剩余: %d）", pendingCount)
			break
		}

		totalSynced += syncedInThisBatch
		logx.Infof("✅ 本批次同步: %d 条", syncedInThisBatch)
	}

	remainingCount, _ := p.GetPendingSyncCount()
	if remainingCount > 0 {
		return fmt.Errorf("强制同步未完成，剩余 %d 条未同步数据", remainingCount)
	}

	logx.Infof("✅ 强制同步完成，共同步 %d 条数据", totalSynced)
	return nil
}

// ForceSyncAsync 异步强制同步（不阻塞）
func (p *PrefixedBadgerDB[T]) ForceSyncAsync() {
	go func() {
		if err := p.ForceSyncAll(); err != nil {
			logx.Errorf("异步强制同步失败: %v", err)
		}
	}()
}

// GetSyncStatus 获取同步状态
func (p *PrefixedBadgerDB[T]) GetSyncStatus() SyncStatus {
	p.closeMu.RLock()
	defer p.closeMu.RUnlock()

	pendingCount, _ := p.GetPendingSyncCount()
	totalCount := p.getTotalCount()

	return SyncStatus{
		PendingCount:   pendingCount,
		TotalCount:     totalCount,
		SyncedCount:    totalCount - pendingCount,
		SyncInProgress: p.syncInProgress,
		IsClosed:       p.closed,
	}
}

// getTotalCount 获取总数据量
func (p *PrefixedBadgerDB[T]) getTotalCount() int {
	count := 0
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte(p.prefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}
		return nil
	})

	if err != nil {
		logx.Errorf("获取总数失败: %v", err)
		return 0
	}

	return count
}

// SyncStatus 同步状态
type SyncStatus struct {
	PendingCount   int  // 待同步数量
	TotalCount     int  // 总数据量
	SyncedCount    int  // 已同步数量
	SyncInProgress bool // 是否正在同步
	IsClosed       bool // 是否已关闭
}
