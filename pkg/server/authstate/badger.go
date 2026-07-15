package authstate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/server/types"
)

const (
	badgerRecordVersion = 1
	statePrefix         = "state/v1/"
	eventPrefix         = "event/v1/"
	hookPrefix          = "hook/v1/"
	badgerUpdateRetries = 32
)

type stateRecord struct {
	Version int   `json:"version"`
	State   State `json:"state"`
}

type eventRecord struct {
	Version          int    `json:"version"`
	Fingerprint      string `json:"fingerprint"`
	Applied          bool   `json:"applied"`
	Generation       uint64 `json:"generation"`
	ControlPublished bool   `json:"control_published"`
	State            State  `json:"state"`
}

type hookRecord struct {
	Version int         `json:"version"`
	Hook    PendingHook `json:"hook"`
}

// BadgerStore 是单节点模式的权威存储，也是共享模式的已确认快照与 Hook 重试存储。
type BadgerStore struct {
	db        *badger.DB
	closeOnce sync.Once
	closeErr  error
}

func OpenBadgerStore(path string) (*BadgerStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("Badger撤销存储路径不能为空")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return nil, fmt.Errorf("创建Badger撤销存储目录失败: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return nil, fmt.Errorf("设置Badger撤销存储权限失败: %w", err)
	}
	db, err := badger.Open(badger.DefaultOptions(path).WithLogger(nil))
	if err != nil {
		return nil, fmt.Errorf("打开Badger撤销存储失败: %w", err)
	}
	return &BadgerStore{db: db}, nil
}

func (s *BadgerStore) Current(ctx context.Context, key IdentityKey) (State, error) {
	if err := key.validate(); err != nil {
		return State{}, err
	}
	if err := contextError(ctx); err != nil {
		return State{}, err
	}
	state := State{Key: key}
	err := s.db.View(func(txn *badger.Txn) error {
		record, err := loadStateRecord(txn, badgerStateKey(key))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		state = record.State
		return nil
	})
	return state, err
}

func (s *BadgerStore) Apply(ctx context.Context, event types.CasdoorEvent, retention time.Duration) (ApplyResult, error) {
	transition, err := validateEvent(event)
	if err != nil {
		return ApplyResult{}, err
	}
	if retention <= 0 {
		return ApplyResult{}, errors.New("Casdoor事件保留时间必须大于0")
	}
	key := eventIdentityKey(event)
	fingerprint, err := eventFingerprint(event)
	if err != nil {
		return ApplyResult{}, err
	}
	var result ApplyResult
	err = s.update(ctx, func(txn *badger.Txn) error {
		existing, loadErr := loadEventRecord(txn, badgerEventKey(event))
		if loadErr == nil {
			if existing.Fingerprint != fingerprint {
				return ErrInvalidEvent
			}
			result = ApplyResult{Generation: existing.Generation, ControlPublished: existing.ControlPublished, State: existing.State}
			return nil
		}
		if !errors.Is(loadErr, badger.ErrKeyNotFound) {
			return loadErr
		}
		state, stateErr := loadState(txn, key)
		if stateErr != nil {
			return stateErr
		}
		applied := true
		if event.EventOrder > 0 && state.EventOrder > 0 && event.EventOrder <= state.EventOrder {
			applied = false
		} else {
			if transition.increment {
				state.Generation++
			}
			if transition.block {
				state.Blocked = true
			}
			if event.EventOrder > 0 {
				state.EventOrder = event.EventOrder
			}
			if event.UID != "" {
				state.UID = event.UID
			}
			state.UpdatedAt = time.Now().UTC()
			if err := setStateRecord(txn, state); err != nil {
				return err
			}
		}
		record := eventRecord{Version: badgerRecordVersion, Fingerprint: fingerprint, Applied: applied, Generation: state.Generation, State: state}
		encoded, encodeErr := json.Marshal(record)
		if encodeErr != nil {
			return encodeErr
		}
		if err := txn.SetEntry(badger.NewEntry(badgerEventKey(event), encoded).WithTTL(retention)); err != nil {
			return err
		}
		result = ApplyResult{Applied: applied, Generation: state.Generation, State: state}
		return nil
	})
	return result, err
}

func (s *BadgerStore) ConfirmActive(ctx context.Context, key IdentityKey, expectedGeneration uint64) (State, error) {
	if err := key.validate(); err != nil {
		return State{}, err
	}
	var state State
	err := s.update(ctx, func(txn *badger.Txn) error {
		current, err := loadState(txn, key)
		if err != nil {
			return err
		}
		if current.Generation != expectedGeneration {
			return ErrGenerationChanged
		}
		current.Blocked = false
		current.UpdatedAt = time.Now().UTC()
		if err := setStateRecord(txn, current); err != nil {
			return err
		}
		state = current
		return nil
	})
	return state, err
}

func (s *BadgerStore) SaveSnapshot(ctx context.Context, state State) error {
	if err := state.Key.validate(); err != nil {
		return err
	}
	return s.update(ctx, func(txn *badger.Txn) error {
		current, err := loadState(txn, state.Key)
		if err != nil {
			return err
		}
		if current.Generation > state.Generation ||
			(current.Generation == state.Generation && current.EventOrder > state.EventOrder) {
			return nil
		}
		state.UpdatedAt = time.Now().UTC()
		return setStateRecord(txn, state)
	})
}

func (s *BadgerStore) MarkControlPublished(ctx context.Context, event types.CasdoorEvent) error {
	return s.update(ctx, func(txn *badger.Txn) error {
		item, err := txn.Get(badgerEventKey(event))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrEventNotFound
		}
		if err != nil {
			return err
		}
		encoded, err := item.ValueCopy(nil)
		if err != nil {
			return err
		}
		record := eventRecord{}
		if err := json.Unmarshal(encoded, &record); err != nil || record.Version != badgerRecordVersion {
			return errors.New("Badger Casdoor事件记录版本无效")
		}
		record.ControlPublished = true
		encoded, err = json.Marshal(record)
		if err != nil {
			return err
		}
		entry := badger.NewEntry(badgerEventKey(event), encoded)
		if expiresAt := item.ExpiresAt(); expiresAt > 0 {
			remaining := time.Until(time.Unix(int64(expiresAt), 0))
			if remaining <= 0 {
				return ErrEventNotFound
			}
			entry = entry.WithTTL(remaining)
		}
		return txn.SetEntry(entry)
	})
}

func (s *BadgerStore) SavePendingHook(ctx context.Context, hook PendingHook) error {
	if strings.TrimSpace(hook.ID) == "" {
		return errors.New("Pending Hook ID不能为空")
	}
	record := hookRecord{Version: badgerRecordVersion, Hook: hook}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return s.update(ctx, func(txn *badger.Txn) error { return txn.Set(badgerHookKey(hook.ID), encoded) })
}

func (s *BadgerStore) PendingHooks(ctx context.Context, limit int) ([]PendingHook, error) {
	if limit <= 0 {
		return nil, nil
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	result := make([]PendingHook, 0, limit)
	err := s.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = []byte(hookPrefix)
		iterator := txn.NewIterator(options)
		defer iterator.Close()
		for iterator.Seek([]byte(hookPrefix)); iterator.Valid(); iterator.Next() {
			encoded, err := iterator.Item().ValueCopy(nil)
			if err != nil {
				return err
			}
			record := hookRecord{}
			if err := json.Unmarshal(encoded, &record); err != nil || record.Version != badgerRecordVersion {
				return errors.New("Badger Pending Hook记录版本无效")
			}
			result = append(result, record.Hook)
		}
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].NextAttempt.Before(result[j].NextAttempt) })
	if len(result) > limit {
		result = result[:limit]
	}
	return result, err
}

func (s *BadgerStore) AckHook(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("Pending Hook ID不能为空")
	}
	return s.update(ctx, func(txn *badger.Txn) error { return txn.Delete(badgerHookKey(id)) })
}

func (s *BadgerStore) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() { s.closeErr = s.db.Close() })
	return s.closeErr
}

func (s *BadgerStore) update(ctx context.Context, fn func(*badger.Txn) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	var err error
	for range badgerUpdateRetries {
		err = s.db.Update(fn)
		if !errors.Is(err, badger.ErrConflict) {
			return err
		}
		if contextErr := contextError(ctx); contextErr != nil {
			return contextErr
		}
	}
	return err
}

func loadState(txn *badger.Txn, key IdentityKey) (State, error) {
	record, err := loadStateRecord(txn, badgerStateKey(key))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return State{Key: key}, nil
	}
	if err != nil {
		return State{}, err
	}
	return record.State, nil
}

func loadStateRecord(txn *badger.Txn, key []byte) (stateRecord, error) {
	item, err := txn.Get(key)
	if err != nil {
		return stateRecord{}, err
	}
	encoded, err := item.ValueCopy(nil)
	if err != nil {
		return stateRecord{}, err
	}
	record := stateRecord{}
	if err := json.Unmarshal(encoded, &record); err != nil {
		return stateRecord{}, err
	}
	if record.Version != badgerRecordVersion {
		return stateRecord{}, errors.New("Badger身份状态记录版本无效")
	}
	return record, nil
}

func loadEventRecord(txn *badger.Txn, key []byte) (eventRecord, error) {
	item, err := txn.Get(key)
	if err != nil {
		return eventRecord{}, err
	}
	encoded, err := item.ValueCopy(nil)
	if err != nil {
		return eventRecord{}, err
	}
	record := eventRecord{}
	if err := json.Unmarshal(encoded, &record); err != nil {
		return eventRecord{}, err
	}
	if record.Version != badgerRecordVersion {
		return eventRecord{}, errors.New("Badger Casdoor事件记录版本无效")
	}
	return record, nil
}

func setStateRecord(txn *badger.Txn, state State) error {
	encoded, err := json.Marshal(stateRecord{Version: badgerRecordVersion, State: state})
	if err != nil {
		return err
	}
	return txn.Set(badgerStateKey(state.Key), encoded)
}

func badgerStateKey(key IdentityKey) []byte { return []byte(statePrefix + key.encoded()) }

func badgerEventKey(event types.CasdoorEvent) []byte {
	return []byte(eventPrefix + eventIdentityKey(event).encoded() + "/" + base64.RawURLEncoding.EncodeToString([]byte(event.ID)))
}

func badgerHookKey(id string) []byte {
	return []byte(hookPrefix + base64.RawURLEncoding.EncodeToString([]byte(id)))
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context不能为nil")
	}
	return ctx.Err()
}
