// Package nosql 提供可靠 store 的只读有界续页；不修改 pending、写回状态或持久格式。
package nosql

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/json"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

const (
	// LocalScanPageMaxItems 是单页允许请求的最大可见模型数量。
	LocalScanPageMaxItems = 1000
	// LocalScanPageMaxVisitedKeys 是单页最多访问的当前可见物理键数，不含 Badger 内部 MVCC 跳过。
	LocalScanPageMaxVisitedKeys = 4096
	// LocalScanPageMaxBytes 是单页物理键与编码 wrapper 的累计读取预算；不是解码后堆内存上限。
	LocalScanPageMaxBytes    = 8 << 20
	localScanCursorMaxLength = 128 << 10
)

var (
	// ErrInvalidLocalScanPage 表示 limit 或游标格式、版本、store/前缀范围无效。
	ErrInvalidLocalScanPage = errors.New("本地分页扫描参数或游标无效")
	// ErrLocalScanItemTooLarge 表示单条编码记录超过单页字节预算，调用方不能跳过该记录继续恢复。
	ErrLocalScanItemTooLarge = errors.New("本地分页扫描单项超过字节预算")
	// ErrLocalScanData 表示本地原件读取或解码失败；整页不返回可继续的游标。
	ErrLocalScanData = errors.New("本地分页扫描原件读取或解码失败")
)

// ScanLocalPage 按 key 升序读取本地可见模型；after 为空开始，nextCursor 为空才表示范围结束。
// limit 必须为 1..LocalScanPageMaxItems，访问键数及编码字节另受固定预算约束，可返回空页和非空游标。
// 游标只可原样用于同 store 路径、模型与 prefix，不是授权凭证；每页独立快照，不保证并发写入时跨页一致。
// 出错时丢弃整页结果与游标；不确认、删除或改变任何原件，旧 ScanLocal 的零值语义不变。
func (store *ReliableWriteStore[T]) ScanLocalPage(ctx context.Context, prefix, after string, limit int) ([]*T, string, error) {
	if err := store.checkReadable(ctx); err != nil {
		return nil, "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 || limit > LocalScanPageMaxItems {
		return nil, "", ErrInvalidLocalScanPage
	}
	scope := []byte(store.db.prefix + prefix)
	binding, err := store.localScanBinding(prefix)
	if err != nil {
		return nil, "", err
	}
	last, err := decodeLocalScanCursor(after, binding, scope)
	if err != nil {
		return nil, "", err
	}
	items := make([]*T, 0, limit)
	next := ""
	err = store.db.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = scope
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		start := scope
		if len(last) != 0 {
			start = last
		}
		it.Seek(start)
		if len(last) != 0 && it.ValidForPrefix(scope) && bytes.Equal(it.Item().Key(), last) {
			it.Next()
		}
		visited, used := 0, int64(0)
		for ; it.ValidForPrefix(scope); it.Next() {
			if err := store.checkReadable(ctx); err != nil {
				return err
			}
			if len(items) == limit || visited == LocalScanPageMaxVisitedKeys {
				next = encodeLocalScanCursor(binding, last)
				return nil
			}
			item := it.Item()
			size := int64(len(item.Key())) + item.ValueSize()
			if size > LocalScanPageMaxBytes {
				return ErrLocalScanItemTooLarge
			}
			if used+size > LocalScanPageMaxBytes {
				next = encodeLocalScanCursor(binding, last)
				return nil
			}
			var wrapper SyncQueueItem[T]
			if err := item.Value(func(value []byte) error { return json.Unmarshal(value, &wrapper) }); err != nil {
				return fmt.Errorf("%w: %w", ErrLocalScanData, err)
			}
			if !wrapper.IsDeleted {
				if wrapper.Item == nil {
					return ErrLocalScanData
				}
				if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
					hook.NewModel()
				}
				items = append(items, wrapper.Item)
			}
			last = item.KeyCopy(last[:0])
			visited++
			used += size
		}
		return nil
	})
	if err == nil {
		err = store.checkReadable(ctx)
	}
	if err != nil {
		return nil, "", err
	}
	return items, next, nil
}

func (store *ReliableWriteStore[T]) localScanBinding(prefix string) ([sha256.Size]byte, error) {
	path, err := filepath.Abs(store.config.BasePath)
	if err != nil {
		return [sha256.Size]byte{}, ErrInvalidLocalScanPage
	}
	hash := sha256.New()
	for _, part := range []string{path, store.db.prefix, prefix} {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		hash.Write(length[:])
		hash.Write([]byte(part))
	}
	var binding [sha256.Size]byte
	copy(binding[:], hash.Sum(nil))
	return binding, nil
}

func encodeLocalScanCursor(binding [sha256.Size]byte, last []byte) string {
	raw := make([]byte, 1+sha256.Size+len(last))
	raw[0] = 1
	copy(raw[1:], binding[:])
	copy(raw[1+sha256.Size:], last)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeLocalScanCursor(after string, binding [sha256.Size]byte, scope []byte) ([]byte, error) {
	if after == "" {
		return nil, nil
	}
	if len(after) > localScanCursorMaxLength {
		return nil, ErrInvalidLocalScanPage
	}
	raw, err := base64.RawURLEncoding.DecodeString(after)
	if err != nil || len(raw) <= 1+sha256.Size || raw[0] != 1 {
		return nil, ErrInvalidLocalScanPage
	}
	last := raw[1+sha256.Size:]
	if !bytes.Equal(raw[1:1+sha256.Size], binding[:]) || !bytes.HasPrefix(last, scope) {
		return nil, ErrInvalidLocalScanPage
	}
	return last, nil
}
