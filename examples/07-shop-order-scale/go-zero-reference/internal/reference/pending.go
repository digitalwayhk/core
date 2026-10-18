// 本文件直接使用 Badger 实现原生下单队列、同步落盘合批和提交后删除。
package reference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v3"
)

type pending struct {
	db        *badger.DB
	mu        sync.Mutex
	submit    sync.RWMutex
	closed    bool
	queue     chan *submission
	done      chan struct{}
	depth     atomic.Int64
	admission chan struct{}
	softSince time.Time
	wake      chan struct{}
}

func openPending(path string) (*pending, error) {
	opts := badger.DefaultOptions(path).WithLogger(nil).WithSyncWrites(true).
		WithDetectConflicts(true).WithNumCompactors(2).WithValueLogFileSize(1 << 20).
		WithMemTableSize(16 << 20).WithNumLevelZeroTables(4).WithNumLevelZeroTablesStall(8).WithValueThreshold(1 << 16)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, err
	}
	s := &pending{db: db, queue: make(chan *submission, 1024), done: make(chan struct{}), admission: make(chan struct{}, 500), wake: make(chan struct{}, 1)}
	err = db.View(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte("q:")); it.ValidForPrefix([]byte("q:")); it.Next() {
			s.depth.Add(1)
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	go s.commitLoop()
	return s, nil
}

func localKey(o *Order) []byte {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", o.UserID, o.RequestID)))
	return []byte("p:" + hex.EncodeToString(h[:]))
}
func queueKey(o *Order) []byte { return []byte(fmt.Sprintf("q:%020d", o.ID)) }

func (s *pending) find(user uint, request string) (*Order, error) {
	var out *Order
	err := s.db.View(func(tx *badger.Txn) error {
		i, e := tx.Get(localKey(&Order{UserID: user, RequestID: request}))
		if errors.Is(e, badger.ErrKeyNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		return i.Value(func(b []byte) error { out = &Order{}; return json.Unmarshal(b, out) })
	})
	return out, err
}

func (s *pending) accept(ctx context.Context, o *Order) (*Order, error) {
	if o == nil || o.ID == 0 || o.UserID == 0 || o.RequestID == "" || o.RequestFingerprint == "" {
		return nil, errors.New("订单参数不完整")
	}
	wait, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	select {
	case s.admission <- struct{}{}:
	case <-wait.Done():
		return nil, wait.Err()
	}
	defer func() { <-s.admission }()
	s.mu.Lock()
	depth := s.depth.Load()
	if depth < 10000 {
		s.softSince = time.Time{}
	} else if s.softSince.IsZero() {
		s.softSince = time.Now()
	}
	lsm, vlog := s.db.Size()
	blocked := depth >= 50000 || lsm+vlog >= 1<<30 || (!s.softSince.IsZero() && time.Since(s.softSince) >= 30*time.Second)
	s.mu.Unlock()
	if blocked {
		return nil, errors.New("本地队列背压")
	}
	r := &submission{order: o, done: make(chan outcome, 1)}
	s.submit.RLock()
	if s.closed {
		s.submit.RUnlock()
		return nil, errors.New("队列已关闭")
	}
	select {
	case s.queue <- r:
		s.submit.RUnlock()
	case <-wait.Done():
		s.submit.RUnlock()
		return nil, wait.Err()
	}
	select {
	case result := <-r.done:
		return result.order, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *pending) commitLoop() {
	defer close(s.done)
	for first := range s.queue {
		batch := []*submission{first}
		if len(s.queue) > 0 {
			timer := time.NewTimer(time.Millisecond)
		collect:
			for len(batch) < 128 {
				select {
				case next, ok := <-s.queue:
					if !ok {
						break collect
					}
					batch = append(batch, next)
				case <-timer.C:
					break collect
				}
			}
			timer.Stop()
		}
		s.commit(batch)
	}
}

func (s *pending) commit(batch []*submission) {
	s.mu.Lock()
	results := make([]outcome, len(batch))
	added := int64(0)
	err := s.db.Update(func(tx *badger.Txn) error {
		for n, r := range batch {
			o := r.order
			key := localKey(o)
			i, e := tx.Get(key)
			if e == nil {
				var old Order
				if e = i.Value(func(b []byte) error { return json.Unmarshal(b, &old) }); e != nil {
					return e
				}
				if old.RequestFingerprint != o.RequestFingerprint {
					results[n].err = errors.New("幂等键已用于不同订单请求")
				} else {
					results[n].order = &old
				}
				continue
			}
			if !errors.Is(e, badger.ErrKeyNotFound) {
				return e
			}
			data, e := json.Marshal(o)
			if e != nil {
				return e
			}
			if e = tx.Set(key, data); e != nil {
				return e
			}
			if e = tx.Set(queueKey(o), key); e != nil {
				return e
			}
			results[n].order = o
			added++
		}
		return nil
	})
	if err == nil {
		s.depth.Add(added)
	}
	s.mu.Unlock()
	for n, r := range batch {
		if err != nil {
			results[n] = outcome{err: err}
		}
		r.done <- results[n]
	}
	if err == nil {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (s *pending) batch(limit int) ([]*Order, error) {
	items := make([]*Order, 0, limit)
	err := s.db.View(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte("q:")); it.ValidForPrefix([]byte("q:")) && len(items) < limit; it.Next() {
			key, e := it.Item().ValueCopy(nil)
			if e != nil {
				return e
			}
			value, e := tx.Get(key)
			if e != nil {
				return e
			}
			o := &Order{}
			if e = value.Value(func(b []byte) error { return json.Unmarshal(b, o) }); e != nil {
				return e
			}
			items = append(items, o)
		}
		return nil
	})
	return items, err
}

func (s *pending) confirm(items []*Order) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := int64(0)
	err := s.db.Update(func(tx *badger.Txn) error {
		for _, o := range items {
			_, e := tx.Get(queueKey(o))
			if errors.Is(e, badger.ErrKeyNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			if e = tx.Delete(localKey(o)); e != nil {
				return e
			}
			if e = tx.Delete(queueKey(o)); e != nil {
				return e
			}
			removed++
		}
		return nil
	})
	if err == nil {
		s.depth.Add(-removed)
	}
	return err
}

func (s *pending) close() error {
	s.submit.Lock()
	if s.closed {
		s.submit.Unlock()
		return nil
	}
	s.closed = true
	close(s.queue)
	s.submit.Unlock()
	<-s.done
	return s.db.Close()
}
