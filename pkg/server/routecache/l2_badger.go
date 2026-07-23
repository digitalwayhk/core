package routecache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/server/config"
)

const badgerL2KeyPrefix = "routecache/v1/"

var (
	ErrBadgerL2Closed   = errors.New("badger route cache l2 closed")
	ErrBadgerL2Capacity = errors.New("badger route cache l2 capacity reached")
)

type cacheEnvelope struct {
	Version   int             `json:"version"`
	ExpiresAt int64           `json:"expires_at"`
	Data      json.RawMessage `json:"data"`
}

type BadgerL2 struct {
	mu       sync.RWMutex
	db       *badger.DB
	path     string
	maxBytes int64
	closed   bool
}

func NewBadgerL2(cfg config.RouteCacheL2Config) (*BadgerL2, error) {
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, errors.New("routeCache.l2.path is required")
	}
	if err := os.MkdirAll(cfg.Path, 0o700); err != nil {
		return nil, err
	}
	open := func() (*badger.DB, error) {
		return badger.Open(badger.DefaultOptions(cfg.Path).WithLogger(nil))
	}
	db, err := open()
	if err != nil && cfg.CorruptionPolicy == "reset" {
		if removeErr := os.RemoveAll(cfg.Path); removeErr != nil {
			return nil, removeErr
		}
		if mkdirErr := os.MkdirAll(cfg.Path, 0o700); mkdirErr != nil {
			return nil, mkdirErr
		}
		db, err = open()
	}
	if err != nil {
		return nil, err
	}
	return &BadgerL2{db: db, path: cfg.Path, maxBytes: cfg.MaxBytes}, nil
}

func (b *BadgerL2) Get(key string) (json.RawMessage, bool, error) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return nil, false, ErrBadgerL2Closed
	}
	var encoded []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(badgerL2Key(key))
		if err != nil {
			return err
		}
		encoded, err = item.ValueCopy(nil)
		return err
	})
	b.mu.RUnlock()
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	envelope := cacheEnvelope{}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.Version != 1 {
		return nil, false, errors.New("unsupported badger route cache envelope version")
	}
	if envelope.ExpiresAt <= time.Now().UnixNano() {
		_ = b.Delete(key)
		return nil, false, nil
	}
	return append(json.RawMessage(nil), envelope.Data...), true, nil
}

func (b *BadgerL2) Set(key string, value json.RawMessage, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("badger route cache ttl must be positive")
	}
	envelope, err := json.Marshal(cacheEnvelope{
		Version:   1,
		ExpiresAt: time.Now().Add(ttl).UnixNano(),
		Data:      append(json.RawMessage(nil), value...),
	})
	if err != nil {
		return err
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrBadgerL2Closed
	}
	if b.maxBytes > 0 {
		lsm, vlog := b.db.Size()
		if lsm+vlog+int64(len(envelope)) > b.maxBytes {
			return ErrBadgerL2Capacity
		}
	}
	storageTTL := ttl
	if storageTTL < time.Second {
		storageTTL = time.Second
	}
	entry := badger.NewEntry(badgerL2Key(key), envelope).WithTTL(storageTTL)
	return b.db.Update(func(txn *badger.Txn) error { return txn.SetEntry(entry) })
}

func (b *BadgerL2) Delete(key string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrBadgerL2Closed
	}
	return b.db.Update(func(txn *badger.Txn) error { return txn.Delete(badgerL2Key(key)) })
}

func (b *BadgerL2) DeletePrefix(prefix string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrBadgerL2Closed
	}
	fullPrefix := badgerL2Key(prefix)
	keys := make([][]byte, 0)
	if err := b.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = fullPrefix
		iterator := txn.NewIterator(options)
		defer iterator.Close()
		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			keys = append(keys, append([]byte(nil), iterator.Item().Key()...))
		}
		return nil
	}); err != nil {
		return err
	}
	return b.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			if err := txn.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *BadgerL2) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	return b.db.Close()
}

func badgerL2Key(key string) []byte {
	return []byte(filepath.ToSlash(badgerL2KeyPrefix + key))
}
