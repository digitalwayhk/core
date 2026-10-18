// 本文件验证隔离副本的版本迁移、TTL、删除及升级后新增事实回退；禁止覆盖目标目录。
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/dgraph-io/badger/v3"
)

func compat(mode, path, backup string, encrypted, post bool) (err error) {
	if mode == "create" || mode == "load" {
		if _, e := os.Stat(path); !os.IsNotExist(e) {
			return errors.New("兼容测试禁止覆盖目标")
		}
	}
	opts := badger.DefaultOptions(path).WithLogger(nil).WithSyncWrites(true).WithDetectConflicts(true)
	if encrypted {
		opts = opts.WithEncryptionKey([]byte("0123456789abcdef0123456789abcdef")).WithIndexCacheSize(16 << 20)
	}
	db, err := badger.Open(opts)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if mode == "create" {
		for version := 1; version <= 2; version++ {
			if e := db.Update(func(tx *badger.Txn) error {
				for id := 0; id < 1000; id++ {
					if e := tx.Set([]byte(fmt.Sprintf("fact:%04d", id)), []byte(fmt.Sprintf("value:%04d:v%d", id, version))); e != nil {
						return e
					}
				}
				return nil
			}); e != nil {
				return e
			}
		}
		if e := db.Update(func(tx *badger.Txn) error {
			for id := 0; id < 1000; id += 4 {
				if e := tx.Delete([]byte(fmt.Sprintf("fact:%04d", id))); e != nil {
					return e
				}
			}
			if e := tx.SetEntry(badger.NewEntry([]byte("expired"), []byte("expired")).WithTTL(0)); e != nil {
				return e
			}
			entry := badger.NewEntry([]byte("expired"), []byte("expired"))
			entry.ExpiresAt = 1
			if e := tx.SetEntry(entry); e != nil {
				return e
			}
			entry = badger.NewEntry([]byte("ttl-live"), []byte("live"))
			entry.ExpiresAt = 4102444800
			return tx.SetEntry(entry)
		}); e != nil {
			return e
		}
	}
	if mode == "load" {
		file, e := os.Open(backup)
		if e != nil {
			return e
		}
		e = db.Load(file, 256)
		closeErr := file.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if mode == "post-write" {
		if e := db.Update(func(tx *badger.Txn) error { return tx.Set([]byte("post-upgrade"), []byte("new-confirmed-fact")) }); e != nil {
			return e
		}
		post = true
	}
	if e := db.Sync(); e != nil {
		return e
	}
	hash := sha256.New()
	err = db.View(func(tx *badger.Txn) error {
		for id := 0; id < 1000; id++ {
			key := []byte(fmt.Sprintf("fact:%04d", id))
			item, e := tx.Get(key)
			if id%4 == 0 {
				if !errors.Is(e, badger.ErrKeyNotFound) {
					return fmt.Errorf("删除键重新可见: %d", id)
				}
				continue
			}
			if e != nil {
				return e
			}
			value, e := item.ValueCopy(nil)
			if e != nil {
				return e
			}
			if string(value) != fmt.Sprintf("value:%04d:v2", id) {
				return errors.New("最新值不匹配")
			}
			fmt.Fprintf(hash, "%s:%s:%d:%d\n", key, value, item.Version(), item.ExpiresAt())
		}
		if _, e := tx.Get([]byte("expired")); !errors.Is(e, badger.ErrKeyNotFound) {
			return errors.New("过期记录可见")
		}
		item, e := tx.Get([]byte("ttl-live"))
		if e != nil {
			return e
		}
		if item.ExpiresAt() != 4102444800 {
			return errors.New("TTL元数据丢失")
		}
		if post {
			item, e := tx.Get([]byte("post-upgrade"))
			if e != nil {
				return e
			}
			value, e := item.ValueCopy(nil)
			if e != nil {
				return e
			}
			if string(value) != "new-confirmed-fact" {
				return errors.New("升级后事实丢失")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	emit(map[string]any{"phase": "compat", "mode": mode, "encrypted": encrypted, "latestVersionHash": hex.EncodeToString(hash.Sum(nil)), "postUpgradeFact": post, "passed": true})
	if backup != "" && mode != "load" && !strings.HasPrefix(mode, "check") {
		file, e := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = db.Backup(file, 0)
		closeErr := file.Close()
		if e != nil {
			return e
		}
		return closeErr
	}
	return nil
}
