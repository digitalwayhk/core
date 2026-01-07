package nosql

import (
	"encoding/json"
	"fmt"
	"time"

	"go.etcd.io/bbolt"
)

type BoltDB struct {
	db   *bbolt.DB
	path string
}

func NewBoltDB(path string) (*BoltDB, error) {
	db, err := bbolt.Open(path, 0600, &bbolt.Options{
		Timeout:         1 * time.Second,
		NoGrowSync:      false, // ✅ 每次扩容都 sync
		NoFreelistSync:  false, // ✅ 每次都持久化空闲列表
		FreelistType:    bbolt.FreelistArrayType,
		ReadOnly:        false,
		MmapFlags:       0,
		InitialMmapSize: 10 << 20, // 10MB 初始映射
		PageSize:        4096,     // 4KB 页
		NoSync:          false,    // ✅ 强制 fsync
		OpenFile:        nil,
	})

	if err != nil {
		return nil, err
	}

	return &BoltDB{db: db, path: path}, nil
}

// 批量写入
func (b *BoltDB) BatchWrite(bucket string, items map[string]interface{}) error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		bkt, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}

		for key, value := range items {
			data, _ := json.Marshal(value)
			if err := bkt.Put([]byte(key), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// 读取
func (b *BoltDB) Get(bucket, key string, result interface{}) error {
	return b.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucket))
		if bkt == nil {
			return bbolt.ErrBucketNotFound
		}

		val := bkt.Get([]byte(key))
		if val == nil {
			return fmt.Errorf("key not found: %s", key)
		}

		return json.Unmarshal(val, result)
	})
}

// 范围查询
func (b *BoltDB) Scan(bucket, prefix string, limit int) ([][]byte, error) {
	var results [][]byte

	err := b.db.View(func(tx *bbolt.Tx) error {
		bkt := tx.Bucket([]byte(bucket))
		if bkt == nil {
			return nil
		}

		c := bkt.Cursor()
		count := 0

		for k, v := c.Seek([]byte(prefix)); k != nil && count < limit; k, v = c.Next() {
			if !hasPrefix(k, []byte(prefix)) {
				break
			}
			dst := make([]byte, len(v))
			copy(dst, v)
			results = append(results, dst)
			count++
		}
		return nil
	})

	return results, err
}

func hasPrefix(s, prefix []byte) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := range prefix {
		if s[i] != prefix[i] {
			return false
		}
	}
	return true
}
