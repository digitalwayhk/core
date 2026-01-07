package nosql

import (
	"encoding/json"
	"time"

	"github.com/cockroachdb/pebble"
)

type PebbleDB struct {
	db   *pebble.DB
	path string
}

func NewPebbleDB(path string) (*PebbleDB, error) {
	opts := &pebble.Options{
		Cache:                       pebble.NewCache(128 << 20), // 128MB
		MemTableSize:                64 << 20,                   // 64MB
		MemTableStopWritesThreshold: 4,
		L0CompactionThreshold:       2,
		L0StopWritesThreshold:       12,
		MaxOpenFiles:                1000,
		DisableWAL:                  false, // ✅ 启用 WAL
	}

	db, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}

	return &PebbleDB{db: db, path: path}, nil
}
func (p *PebbleDB) WALMinSyncInterval() time.Duration {
	return 0
}

// 批量写入
func (p *PebbleDB) BatchWrite(items map[string]interface{}) error {
	batch := p.db.NewBatch()
	defer batch.Close()

	for key, value := range items {
		data, _ := json.Marshal(value)
		if err := batch.Set([]byte(key), data, pebble.Sync); err != nil {
			return err
		}
	}

	return batch.Commit(pebble.Sync)
}

// 读取
func (p *PebbleDB) Get(key string, result interface{}) error {
	data, closer, err := p.db.Get([]byte(key))
	if err != nil {
		return err
	}
	defer closer.Close()

	return json.Unmarshal(data, result)
}

// 范围查询
func (p *PebbleDB) Scan(prefix string, limit int) ([][]byte, error) {
	var results [][]byte

	iter := p.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "\xff"),
	})
	defer iter.Close()

	count := 0
	for iter.First(); iter.Valid() && count < limit; iter.Next() {
		value := iter.Value()
		dst := make([]byte, len(value))
		copy(dst, value)
		results = append(results, dst)
		count++
	}

	return results, iter.Error()
}
