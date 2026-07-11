package nosql

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	corejson "github.com/digitalwayhk/core/pkg/json"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

type memoryActionState struct {
	mu               sync.RWMutex
	rows             map[string]interface{}
	failTransaction  atomic.Bool
	failCommit       atomic.Bool
	transactionFails atomic.Int32
	commitFailures   atomic.Int32
	transactionErr   error
	commitErr        error
	operationErrors  map[string][]error
	operationCalls   map[string]int
	existsResults    []memoryExistsResult
	existsCalls      int
}

type memoryExistsResult struct {
	exists bool
	err    error
}

type memoryAction struct {
	state       *memoryActionState
	inTx        bool
	stagedRows  map[string]interface{}
	stagedDrops map[string]struct{}
	operation   string
	fatalErr    error
	fatalAt     int
	calls       *atomic.Int32
}

func newMemoryAction() *memoryAction {
	return &memoryAction{state: &memoryActionState{
		rows:            make(map[string]interface{}),
		operationErrors: make(map[string][]error),
		operationCalls:  make(map[string]int),
	}}
}

func (a *memoryAction) Clone() types.IDataAction {
	return &memoryAction{
		state:     a.state,
		operation: a.operation,
		fatalErr:  a.fatalErr,
		fatalAt:   a.fatalAt,
		calls:     a.calls,
	}
}

func (a *memoryAction) GetMaxOpenConns() int { return 16 }

func (a *memoryAction) GetSyncPoolKey(data interface{}) string { return "memory" }

func (a *memoryAction) setFailTransaction(enabled bool) { a.state.failTransaction.Store(enabled) }

func (a *memoryAction) setFailCommit(enabled bool) { a.state.failCommit.Store(enabled) }

func (a *memoryAction) setTransactionError(err error) {
	a.state.mu.Lock()
	a.state.transactionErr = err
	a.state.mu.Unlock()
}

func (a *memoryAction) setCommitError(err error) {
	a.state.mu.Lock()
	a.state.commitErr = err
	a.state.mu.Unlock()
}

func (a *memoryAction) scriptOperation(operation string, errs ...error) {
	a.state.mu.Lock()
	a.state.operationErrors[operation] = append([]error(nil), errs...)
	a.state.operationCalls[operation] = 0
	a.state.mu.Unlock()
}

func (a *memoryAction) operationCallCount(operation string) int {
	a.state.mu.RLock()
	defer a.state.mu.RUnlock()
	return a.state.operationCalls[operation]
}

func (a *memoryAction) scriptExists(results ...memoryExistsResult) {
	a.state.mu.Lock()
	a.state.existsResults = append([]memoryExistsResult(nil), results...)
	a.state.existsCalls = 0
	a.state.mu.Unlock()
}

func (a *memoryAction) withFatal(operation string, fatalAt int, err error, calls *atomic.Int32) *memoryAction {
	a.operation = operation
	a.fatalAt = fatalAt
	a.fatalErr = err
	a.calls = calls
	return a
}

func (a *memoryAction) key(data interface{}) string {
	row, ok := data.(types.IRowCode)
	if !ok {
		return fmt.Sprintf("%T:%v", data, data)
	}
	dbName := ""
	if named, ok := data.(types.IDBName); ok {
		dbName = named.GetRemoteDBName()
	}
	return fmt.Sprintf("%T:%s:%s", data, dbName, row.GetHash())
}

func (a *memoryAction) Exists(data interface{}) (bool, error) {
	a.state.mu.Lock()
	if a.state.existsCalls < len(a.state.existsResults) {
		result := a.state.existsResults[a.state.existsCalls]
		a.state.existsCalls++
		a.state.mu.Unlock()
		return result.exists, result.err
	}
	a.state.mu.Unlock()
	key := a.key(data)
	if a.inTx {
		if _, deleted := a.stagedDrops[key]; deleted {
			return false, nil
		}
		if _, exists := a.stagedRows[key]; exists {
			return true, nil
		}
	}
	a.state.mu.RLock()
	defer a.state.mu.RUnlock()
	_, exists := a.state.rows[key]
	return exists, nil
}

func (a *memoryAction) Transaction() error {
	a.state.mu.RLock()
	transactionErr := a.state.transactionErr
	a.state.mu.RUnlock()
	if transactionErr != nil {
		return transactionErr
	}
	if a.state.failTransaction.Load() {
		a.state.transactionFails.Add(1)
		return errors.New("注入的事务开启失败")
	}
	a.inTx = true
	a.stagedRows = make(map[string]interface{})
	a.stagedDrops = make(map[string]struct{})
	return nil
}

func (a *memoryAction) Load(_ *types.SearchItem, _ interface{}) error {
	return errors.New("内存 action 不支持通用查询")
}

func (a *memoryAction) failOperation(operation string) error {
	a.state.mu.Lock()
	callIndex := a.state.operationCalls[operation]
	a.state.operationCalls[operation] = callIndex + 1
	var scriptedErr error
	if callIndex < len(a.state.operationErrors[operation]) {
		scriptedErr = a.state.operationErrors[operation][callIndex]
	}
	a.state.mu.Unlock()
	if scriptedErr != nil {
		return scriptedErr
	}
	if a.operation != operation || a.calls == nil {
		return nil
	}
	call := int(a.calls.Add(1))
	if call >= a.fatalAt {
		return a.fatalErr
	}
	return nil
}

func (a *memoryAction) Insert(data interface{}) error {
	if err := a.failOperation("insert"); err != nil {
		return err
	}
	return a.store(data)
}

func (a *memoryAction) Update(data interface{}) error {
	if err := a.failOperation("update"); err != nil {
		return err
	}
	return a.store(data)
}

func (a *memoryAction) store(data interface{}) error {
	key := a.key(data)
	snapshot, err := snapshotMemoryValue(data)
	if err != nil {
		return fmt.Errorf("创建内存存储快照失败: %w", err)
	}
	data = snapshot
	if a.inTx {
		a.stagedRows[key] = data
		delete(a.stagedDrops, key)
		return nil
	}
	a.state.mu.Lock()
	a.state.rows[key] = data
	a.state.mu.Unlock()
	return nil
}

func snapshotMemoryValue(value interface{}) (interface{}, error) {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil, nil
	}
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return value, nil
	}
	data, err := corejson.Marshal(value)
	if err != nil {
		return nil, err
	}
	if rv.Kind() == reflect.Ptr {
		snapshot := reflect.New(rv.Elem().Type())
		if err := corejson.Unmarshal(data, snapshot.Interface()); err != nil {
			return nil, err
		}
		return snapshot.Interface(), nil
	}
	snapshot := reflect.New(rv.Type())
	if err := corejson.Unmarshal(data, snapshot.Interface()); err != nil {
		return nil, err
	}
	return snapshot.Elem().Interface(), nil
}

func (a *memoryAction) Delete(data interface{}) error {
	if err := a.failOperation("delete"); err != nil {
		return err
	}
	key := a.key(data)
	if a.inTx {
		delete(a.stagedRows, key)
		a.stagedDrops[key] = struct{}{}
		return nil
	}
	a.state.mu.Lock()
	delete(a.state.rows, key)
	a.state.mu.Unlock()
	return nil
}

func (a *memoryAction) Raw(string, interface{}) error  { return nil }
func (a *memoryAction) Exec(string, interface{}) error { return nil }
func (a *memoryAction) GetModelDB(interface{}) (interface{}, error) {
	return nil, nil
}

func (a *memoryAction) Commit() error {
	a.state.mu.RLock()
	commitErr := a.state.commitErr
	a.state.mu.RUnlock()
	if commitErr != nil {
		return commitErr
	}
	if a.state.failCommit.Load() {
		a.state.commitFailures.Add(1)
		return errors.New("注入的事务提交失败")
	}
	a.state.mu.Lock()
	for key := range a.stagedDrops {
		delete(a.state.rows, key)
	}
	for key, value := range a.stagedRows {
		a.state.rows[key] = value
	}
	a.state.mu.Unlock()
	a.inTx = false
	return nil
}

func (a *memoryAction) GetRunDB() interface{} { return nil }

func (a *memoryAction) Rollback() error {
	a.inTx = false
	a.stagedRows = nil
	a.stagedDrops = nil
	return nil
}

func (a *memoryAction) value(data interface{}) (interface{}, bool) {
	a.state.mu.RLock()
	defer a.state.mu.RUnlock()
	value, ok := a.state.rows[a.key(data)]
	return value, ok
}

func memoryValueAs[T any](a *memoryAction, probe interface{}) (*T, bool) {
	value, ok := a.value(probe)
	if !ok {
		return nil, false
	}
	result, ok := value.(*T)
	if ok {
		return result, true
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() == reflect.Ptr && rv.Elem().Type() == reflect.TypeOf(*new(T)) {
		result := rv.Interface().(*T)
		return result, true
	}
	return nil, false
}
