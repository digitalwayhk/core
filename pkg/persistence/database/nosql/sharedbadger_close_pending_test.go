package nosql

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCloseReturnsPendingSyncErrorAndRepeatsIt(t *testing.T) {
	config := newTestConfig(t.TempDir())
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	db.syncLock.Lock()
	db.syncDB = true
	db.syncLock.Unlock()
	require.NoError(t, db.Set(newFund("close-pending", "spot", 1), 0))

	first := db.Close()
	var pending *PendingSyncError
	require.ErrorAs(t, first, &pending)
	require.Equal(t, db.prefix, pending.Prefix)
	require.Equal(t, 1, pending.Count)

	second := db.Close()
	require.EqualError(t, second, first.Error())
}

func TestCloseWithoutPendingDataSucceeds(t *testing.T) {
	config := newTestConfig(t.TempDir())
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)

	require.NoError(t, db.Close())
}

func TestClosePreservesWriteBehindBindingError(t *testing.T) {
	config := DefaultSharedConfig(t.TempDir())
	db, err := NewSharedBadgerDB[testFund](config.Path, config)
	require.NoError(t, err)
	db.SetSyncDB(writeBehindTestList())

	err = db.Close()
	require.ErrorIs(t, err, ErrUnsafeWriteBehindConfig)
	var pending *PendingSyncError
	require.False(t, errors.As(err, &pending))
}
