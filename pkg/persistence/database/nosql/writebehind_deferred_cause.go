// 本文件仅在当前进程有界保留未准入回调的原错误链；不持久化错误或推断业务结果。
package nosql

import (
	"errors"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

type writeBehindCauseIdentity struct {
	id          string
	version     time.Time
	fingerprint string
	attempt     int
	batch       string
}

func dispositionCauseIdentity[T types.IModel](r WriteBehindFailureRecord[T]) writeBehindCauseIdentity {
	identity := writeBehindCauseIdentity{id: r.ID, fingerprint: r.Fingerprint, attempt: r.Attempts, batch: r.BatchID}
	if r.Snapshot != nil {
		identity.version = r.Snapshot.UpdatedAt
	}
	return identity
}

func (a writeBehindCauseIdentity) matches(b writeBehindCauseIdentity) bool {
	return a.id == b.id && a.version.Equal(b.version) && a.fingerprint == b.fingerprint && a.attempt == b.attempt && a.batch == b.batch
}

type writeBehindRetainedCause struct {
	identity writeBehindCauseIdentity
	cause    error
}

type writeBehindCauseRetention struct {
	mu       sync.Mutex
	entries  map[string]writeBehindRetainedCause
	limit    int
	overflow uint64
	closed   bool
}

func (c *writeBehindCauseRetention) remember(identity writeBehindCauseIdentity, cause error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || cause == nil {
		return
	}
	if old, ok := c.entries[identity.id]; ok {
		if old.identity.matches(identity) {
			return
		}
		delete(c.entries, identity.id)
	}
	if len(c.entries) >= c.limit {
		c.overflow++
		return
	}
	if c.entries == nil {
		c.entries = make(map[string]writeBehindRetainedCause)
	}
	c.entries[identity.id] = writeBehindRetainedCause{identity: identity, cause: cause}
}

// take仅把精确当前尝试的引用转移给既有job；失配拒绝交付，不让旧通知删除新引用。
func (c *writeBehindCauseRetention) take(identity writeBehindCauseIdentity) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[identity.id]
	if c.closed || !ok || !entry.identity.matches(identity) {
		return nil
	}
	delete(c.entries, identity.id)
	return entry.cause
}

func (c *writeBehindCauseRetention) forget(identity writeBehindCauseIdentity) {
	c.mu.Lock()
	if entry, ok := c.entries[identity.id]; ok && entry.identity.matches(identity) {
		delete(c.entries, identity.id)
	}
	c.mu.Unlock()
}

func (c *writeBehindCauseRetention) close() {
	c.mu.Lock()
	c.closed = true
	c.entries = nil
	c.mu.Unlock()
}

func (c *writeBehindCauseRetention) metrics() (int, int, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries), c.limit, c.overflow
}

// pruneDispositionCauses只点读至多ScanLimit项；不写元数据、不占本地提交队列、不扫描业务库。
func (p *PrefixedBadgerDB[T]) pruneDispositionCauses() error {
	c := &p.disposition.causes
	c.mu.Lock()
	identities := make([]writeBehindCauseIdentity, 0, len(c.entries))
	for _, entry := range c.entries {
		identities = append(identities, entry.identity)
	}
	c.mu.Unlock()
	if len(identities) == 0 {
		return nil
	}
	var stale []writeBehindCauseIdentity
	err := p.manager.db.View(func(txn *badger.Txn) error {
		for _, identity := range identities {
			var r WriteBehindFailureRecord[T]
			e := dispositionRead(txn, p.dispositionPrefix()+"r:"+identity.id, &r)
			if e != nil && !errors.Is(e, badger.ErrKeyNotFound) {
				return e
			}
			if e != nil || r.State != WriteBehindUnknown || !identity.matches(dispositionCauseIdentity(r)) {
				stale = append(stale, identity)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, identity := range stale {
		if entry, ok := c.entries[identity.id]; ok && entry.identity.matches(identity) {
			delete(c.entries, identity.id)
		}
	}
	return nil
}
