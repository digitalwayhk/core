package nosql

import (
	"errors"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

func writeBehindTestList() *entity.ModelList[testFund] {
	return entity.NewModelList[testFund](nil)
}

func TestEnableWriteBehindAcceptsDurableConfig(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	config.SyncWrites = true
	config.DetectConflicts = true
	config.CorruptionPolicy = CorruptionPolicyFail
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.EnableWriteBehind(writeBehindTestList()))
}

// TestEnableWriteBehindRejectsSecondBinding 验证兼容入口同样拒绝静默重复绑定。
func TestEnableWriteBehindRejectsSecondBinding(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	config.SyncWrites = true
	config.CorruptionPolicy = CorruptionPolicyFail
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, db.EnableWriteBehind(writeBehindTestList()))
	require.Error(t, db.EnableWriteBehind(writeBehindTestList()))
}

func TestEnableWriteBehindRejectsUnsafeConfigs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*BadgerDBConfig)
	}{
		{name: "async writes", mutate: func(c *BadgerDBConfig) { c.SyncWrites = false }},
		{name: "conflicts disabled", mutate: func(c *BadgerDBConfig) { c.DetectConflicts = false }},
		{name: "cache reset", mutate: func(c *BadgerDBConfig) { c.CorruptionPolicy = CorruptionPolicyResetCache }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultSharedConfig(t.TempDir())
			config.SyncWrites = true
			config.DetectConflicts = true
			config.CorruptionPolicy = CorruptionPolicyFail
			tt.mutate(&config)
			db, err := NewSharedBadgerDB[testFund](config.Path, config)
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			err = db.EnableWriteBehind(writeBehindTestList())
			require.ErrorIs(t, err, ErrUnsafeWriteBehindConfig)
			require.False(t, db.syncDB)
		})
	}
}

func TestLegacySetSyncDBMakesUnsafeBindingObservable(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	db.SetSyncDB(writeBehindTestList())
	err = db.Set(newFund("legacy-bind", "spot", 1), 0)
	require.ErrorIs(t, err, ErrUnsafeWriteBehindConfig)
}

func TestWriteBehindRejectsTTLButCacheAllowsIt(t *testing.T) {
	t.Run("write behind", func(t *testing.T) {
		config := DefaultSharedConfig(t.TempDir())
		config.SyncWrites = true
		db, err := NewSharedBadgerDB[testFund](config.Path, config)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		require.NoError(t, db.EnableWriteBehind(writeBehindTestList()))

		err = db.Set(newFund("pending-ttl", "spot", 1), time.Minute)
		require.ErrorIs(t, err, ErrWriteBehindTTL)
	})

	t.Run("cache", func(t *testing.T) {
		config := DefaultSharedConfig(t.TempDir())
		db, err := NewSharedBadgerDB[testFund](config.Path, config)
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		err = db.Set(newFund("cache-ttl", "spot", 1), time.Minute)
		require.NoError(t, err)
		_, err = db.Get("cache-ttl:spot")
		require.False(t, errors.Is(err, ErrWriteBehindTTL))
	})
}
