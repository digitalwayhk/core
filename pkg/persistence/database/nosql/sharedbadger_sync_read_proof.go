// 本文件仅在已完整解码且原件逐字未变时复用队列读证明，不新增持久状态或省略版本核验。
package nosql

import (
	"crypto/sha256"
	"io"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
	corejson "github.com/digitalwayhk/core/pkg/json"
	jsoniter "github.com/json-iterator/go"
)

// 同DB指针+完整物理key+非零Badger MVCC版本约束已解码原件；仍重读并核对实际编码SHA256，不保留正文副本。
type syncQueueReadProof struct {
	database     *badger.DB
	key          string
	version      uint64
	updatedAt    time.Time
	encodingHash [sha256.Size]byte
}

func (proof *syncQueueReadProof) matches(database *badger.DB, key string, version uint64, updatedAt time.Time, encoded []byte) bool {
	return proof != nil && proof.database == database && proof.key == key && proof.version != 0 && proof.version == version && proof.updatedAt.Equal(updatedAt) && proof.encodingHash == sha256.Sum256(encoded)
}

func plainSyncField(key string) bool {
	switch key {
	case "key", "item", "op", "created_at", "updated_at", "is_synced", "handed_off", "synced_at", "is_deleted", "deleted_at":
		return true
	}
	return false
}

// markPlainSyncEnvelope复用现有codec的对象遍历/Skip，直接复制原值，不自行解析或全局替换JSON文本。
func markPlainSyncEnvelope(encoded []byte, now time.Time) ([]byte, bool) {
	codec := jsoniter.ConfigCompatibleWithStandardLibrary
	iter := codec.BorrowIterator(encoded)
	defer codec.ReturnIterator(iter)
	if iter.WhatIsNext() != jsoniter.ObjectValue {
		return nil, false
	}
	stamp, err := corejson.Marshal(now)
	if err != nil {
		return nil, false
	}
	seen := make(map[string]bool, 10)
	result := make([]byte, 0, len(encoded)+48)
	result = append(result, '{')
	ok := iter.ReadObjectCB(func(iter *jsoniter.Iterator, key string) bool {
		if seen[key] {
			return false
		}
		canonical := plainSyncField(key)
		if !canonical {
			for _, name := range []string{"key", "item", "op", "created_at", "updated_at", "is_synced", "handed_off", "synced_at", "is_deleted", "deleted_at"} {
				if strings.EqualFold(key, name) {
					return false
				}
			}
		}
		if len(seen) > 0 {
			result = append(result, ',')
		}
		seen[key] = true
		if canonical {
			result = append(result, '"')
			result = append(result, key...)
			result = append(result, '"')
		} else {
			field, e := corejson.Marshal(key)
			if e != nil {
				return false
			}
			result = append(result, field...)
		}
		result = append(result, ':')
		switch key {
		case "is_synced":
			iter.Skip()
			result = append(result, "true"...)
		case "synced_at":
			iter.Skip()
			result = append(result, stamp...)
		default:
			result = iter.SkipAndAppendBytes(result)
		}
		return iter.Error == nil
	})
	if !ok || iter.Error != nil {
		return nil, false
	}
	if iter.WhatIsNext() != jsoniter.InvalidValue || iter.Error != io.EOF {
		return nil, false
	}
	if !seen["is_synced"] {
		if len(seen) > 0 {
			result = append(result, ',')
		}
		result = append(result, `"is_synced":true`...)
		seen["is_synced"] = true
	}
	if !seen["synced_at"] {
		result = append(result, `,"synced_at":`...)
		result = append(result, stamp...)
	}
	return append(result, '}'), true
}
