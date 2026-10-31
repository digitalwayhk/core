// 本程序用公共API重放07的p/q原子写删，逐键核对成功事务；仅在独占实验目录运行。
package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/dgraph-io/badger/v3"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	path := flag.String("path", "", "独占实验目录，work模式不得已存在")
	mode := flag.String("mode", "natural", "natural、flatten、gc、crash-write或crash-check")
	rounds := flag.Int("rounds", 16, "写入轮数")
	rows := flag.Int("rows", 16384, "每轮事实数")
	payloadSize := flag.Int("payload", 768, "随机payload字节数")
	compactClose := flag.Bool("compact-close", false, "是否关闭时压缩L0")
	compatMode := flag.String("compat", "", "create、check、load或post-write")
	backup := flag.String("backup", "", "备份文件")
	encrypted := flag.Bool("encrypted", false, "使用固定测试密钥，禁止用于业务")
	post := flag.Bool("post", false, "验证升级后新事实")
	flag.Parse()
	if *compatMode != "" {
		return compat(*compatMode, *path, *backup, *encrypted, *post)
	}
	if *path == "" || *rounds < 1 || *rows < 2 || *payloadSize < 1 {
		return errors.New("参数无效")
	}
	if *mode != "natural" && *mode != "flatten" && *mode != "gc" && *mode != "crash-write" && *mode != "crash-check" {
		return errors.New("mode无效")
	}
	if *mode != "crash-check" {
		if _, e := os.Stat(*path); !os.IsNotExist(e) {
			return errors.New("禁止覆盖已有实验目录")
		}
		if e := os.MkdirAll(filepath.Dir(*path), 0700); e != nil {
			return e
		}
	}
	opts := badger.DefaultOptions(*path).WithLogger(nil).WithSyncWrites(true).
		WithDetectConflicts(true).WithMemTableSize(16 << 20).WithNumCompactors(2).
		WithNumLevelZeroTables(4).WithNumLevelZeroTablesStall(8).
		WithValueLogFileSize(1 << 20).WithValueThreshold(1 << 16).WithCompactL0OnClose(*compactClose)
	// gc模式单独降低阈值，使大值进入vlog；这是诊断变量，不冒充现场参数。
	if *mode == "gc" {
		opts = opts.WithValueThreshold(1024)
	}
	db, err := badger.Open(opts)
	if err != nil {
		return err
	}
	defer func() {
		if db != nil {
			err = errors.Join(err, db.Close())
		}
	}()
	effective := db.Opts()
	emit(map[string]any{"phase": "options", "syncWrites": effective.SyncWrites, "detectConflicts": effective.DetectConflicts,
		"memTableSize": effective.MemTableSize, "numMemtables": effective.NumMemtables, "numCompactors": effective.NumCompactors,
		"l0": effective.NumLevelZeroTables, "l0Stall": effective.NumLevelZeroTablesStall, "valueLogFileSize": effective.ValueLogFileSize,
		"valueThreshold": effective.ValueThreshold, "blockCacheSize": effective.BlockCacheSize, "indexCacheSize": effective.IndexCacheSize,
		"compression": effective.Compression, "compactL0OnClose": effective.CompactL0OnClose, "baseLevelSize": effective.BaseLevelSize,
		"baseTableSize": effective.BaseTableSize, "mode": *mode, "rounds": *rounds, "rows": *rows, "payload": *payloadSize, "seed": 20261019})
	want := make(map[string][32]byte)
	rng := rand.New(rand.NewSource(20261019))
	if *mode == "crash-check" {
		for id := 1; id <= *rounds**rows; id++ {
			p, q, value := record(id, *payloadSize, rng)
			if id > (*rounds-1)**rows+*rows/2 {
				want[p] = sha256.Sum256(value)
				want[q] = sha256.Sum256([]byte(p))
			}
		}
		return verify(db, want, "after-hard-kill")
	}
	for round := 0; round < *rounds; round++ {
		for offset := 0; offset < *rows; offset += 128 {
			staged := make([]struct {
				key   string
				value []byte
			}, 0, 256)
			for n := offset; n < min(offset+128, *rows); n++ {
				p, q, value := record(round**rows+n+1, *payloadSize, rng)
				staged = append(staged, struct {
					key   string
					value []byte
				}{p, value}, struct {
					key   string
					value []byte
				}{q, []byte(p)})
			}
			if e := db.Update(func(tx *badger.Txn) error {
				for _, entry := range staged {
					if e := tx.Set([]byte(entry.key), entry.value); e != nil {
						return e
					}
				}
				return nil
			}); e != nil {
				return e
			}
			for _, entry := range staged {
				want[entry.key] = sha256.Sum256(entry.value)
			}
		}
		if round > 0 {
			if e := removeRange(db, want, (round-1)**rows+1, round**rows); e != nil {
				return e
			}
		}
		if e := verify(db, want, fmt.Sprintf("round-%d", round)); e != nil {
			return e
		}
	}
	if e := removeRange(db, want, (*rounds-1)**rows+1, (*rounds-1)**rows+*rows/2); e != nil {
		return e
	}
	if e := verify(db, want, "half-deleted"); e != nil {
		return e
	}
	if *mode == "crash-write" {
		emit(map[string]any{"phase": "crash-ready", "expectedKeys": len(want)})
		for {
			time.Sleep(time.Second)
		}
	}
	if *mode == "flatten" {
		if e := db.Flatten(2); e != nil {
			return e
		}
		if e := verify(db, want, "after-flatten"); e != nil {
			return e
		}
	}
	if *mode == "gc" {
		// 先关闭重开持久化vlog head，再压缩产生discard统计，避免把ErrNoRewrite当有效GC。
		if e := db.Close(); e != nil {
			return e
		}
		db = nil
		db, err = badger.Open(opts)
		if err != nil {
			return err
		}
		// 单个SST不会被Flatten重写；再提交一个相同索引版本并flush，触发有重叠的真实压缩。
		if e := db.Update(func(tx *badger.Txn) error {
			key := []byte(fmt.Sprintf("q:%020d", *rounds**rows))
			item, e := tx.Get(key)
			if e != nil {
				return e
			}
			value, e := item.ValueCopy(nil)
			if e != nil {
				return e
			}
			return tx.Set(key, value)
		}); e != nil {
			return e
		}
		if e := db.Close(); e != nil {
			return e
		}
		db = nil
		db, err = badger.Open(opts)
		if err != nil {
			return err
		}

		if e := db.Flatten(2); e != nil {
			return e
		}
		if e := verify(db, want, "before-gc"); e != nil {
			return e
		}
		success := 0
		for range 100 {
			e := db.RunValueLogGC(0.5)
			if errors.Is(e, badger.ErrNoRewrite) {
				break
			}
			if e != nil {
				return e
			}
			success++
		}
		emit(map[string]any{"phase": "gc", "successfulRewrites": success})
		if e := verify(db, want, "after-gc"); e != nil {
			return e
		}
	}
	if e := db.Close(); e != nil {
		return e
	}
	db = nil
	db, err = badger.Open(opts)
	if err != nil {
		return err
	}
	if e := verify(db, want, "after-reopen"); e != nil {
		return e
	}
	if e := removeRange(db, want, (*rounds-1)**rows+*rows/2+1, *rounds**rows); e != nil {
		return e
	}
	if e := verify(db, want, "empty"); e != nil {
		return e
	}
	if e := db.Close(); e != nil {
		return e
	}
	db = nil
	db, err = badger.Open(opts)
	if err != nil {
		return err
	}
	return verify(db, want, "empty-reopen")
}

func record(id, size int, rng *rand.Rand) (string, string, []byte) {
	request := fmt.Sprintf("repro-%020d", id)
	hash := sha256.Sum256([]byte("7:" + request))
	p := fmt.Sprintf("p:%x", hash)
	q := fmt.Sprintf("q:%020d", id)
	payload := make([]byte, size)
	_, _ = rng.Read(payload)
	value, _ := json.Marshal(map[string]any{"orderID": id, "userID": 7, "requestID": request, "requestFingerprint": request, "payload": payload})
	return p, q, value
}

func removeRange(db *badger.DB, want map[string][32]byte, first, last int) error {
	for start := first; start <= last; start += 512 {
		keys := make([]string, 0, 1024)
		for id := start; id <= min(start+511, last); id++ {
			hash := sha256.Sum256([]byte(fmt.Sprintf("7:repro-%020d", id)))
			keys = append(keys, fmt.Sprintf("p:%x", hash), fmt.Sprintf("q:%020d", id))
		}
		if err := db.Update(func(tx *badger.Txn) error {
			for _, k := range keys {
				if e := tx.Delete([]byte(k)); e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			return err
		}
		for _, k := range keys {
			delete(want, k)
		}
	}
	return nil
}

func verify(db *badger.DB, want map[string][32]byte, phase string) error {
	seen := 0
	err := db.View(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			key := string(it.Item().Key())
			expected, ok := want[key]
			if !ok {
				return fmt.Errorf("%s: 已删除或未知的键重新可见: %x", phase, sha256.Sum256([]byte(key)))
			}
			value, e := it.Item().ValueCopy(nil)
			if e != nil {
				return e
			}
			if sha256.Sum256(value) != expected {
				return fmt.Errorf("%s: 事实值不匹配", phase)
			}
			seen++
		}
		if seen != len(want) {
			return fmt.Errorf("%s: 缺失键: actual=%d expected=%d", phase, seen, len(want))
		}
		return nil
	})
	lsm, vlog := db.Size()
	emit(map[string]any{"phase": phase, "expectedKeys": len(want), "actualKeys": seen, "lsmBytes": lsm, "vlogBytes": vlog, "levels": db.Levels(), "passed": err == nil})
	return err
}

func emit(value any) {
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		panic(err)
	}
}
