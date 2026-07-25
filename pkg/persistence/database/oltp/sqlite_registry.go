package oltp

import "sync"

var sharedSqliteInstances sync.Map

// GetSharedSqliteInstance returns the process-wide SQLite owner for a logical database.
func GetSharedSqliteInstance(name string) *Sqlite {
	if instance, ok := sharedSqliteInstances.Load(name); ok {
		return instance.(*Sqlite)
	}

	instance := NewSqlite()
	instance.Name = name
	actual, _ := sharedSqliteInstances.LoadOrStore(name, instance)
	return actual.(*Sqlite)
}
