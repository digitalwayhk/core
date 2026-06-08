package oltp

import "sync"

// globalSqliteInstances is the single authoritative registry of named Sqlite
// singletons shared across the persistence layer.  Both entity.ModelList and
// adapter.DefaultAdapter must go through GetGlobalSqliteInstance so they always
// get the same *Sqlite handle for a given database name.
var (
	globalSqliteInstances = make(map[string]*Sqlite)
	sqliteInstanceMutex   sync.RWMutex
)

// GetGlobalSqliteInstance returns the process-wide singleton *Sqlite for name,
// creating it on first call.  Concurrent calls for the same name are safe.
func GetGlobalSqliteInstance(name string) *Sqlite {
	sqliteInstanceMutex.RLock()
	if instance, exists := globalSqliteInstances[name]; exists {
		sqliteInstanceMutex.RUnlock()
		return instance
	}
	sqliteInstanceMutex.RUnlock()

	sqliteInstanceMutex.Lock()
	defer sqliteInstanceMutex.Unlock()

	// Double-checked locking.
	if instance, exists := globalSqliteInstances[name]; exists {
		return instance
	}

	instance := NewSqlite()
	instance.Name = name
	globalSqliteInstances[name] = instance
	return instance
}
