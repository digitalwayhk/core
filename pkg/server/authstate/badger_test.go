package authstate

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestBadgerStoreRestoresGenerationAndBlockState(t *testing.T) {
	path := t.TempDir()
	key := testIdentityKey()
	event := testCasdoorEvent("evt-delete", "delete-user", 1, true)

	first, err := OpenBadgerStore(path)
	require.NoError(t, err)
	result, err := first.Apply(context.Background(), event, time.Hour)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NoError(t, first.Close())

	second, err := OpenBadgerStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	state, err := second.Current(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.Generation)
	require.True(t, state.Blocked)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestBadgerApplyIsIdempotentAndKeepsOriginalEventGeneration(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	first := testCasdoorEvent("evt-1", "logout", 1, false)
	result, err := store.Apply(context.Background(), first, time.Hour)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Equal(t, uint64(1), result.Generation)

	_, err = store.Apply(context.Background(), testCasdoorEvent("evt-2", "logout", 2, false), time.Hour)
	require.NoError(t, err)
	retry, err := store.Apply(context.Background(), first, time.Hour)
	require.NoError(t, err)
	require.False(t, retry.Applied)
	require.Equal(t, uint64(1), retry.Generation)
}

func TestBadgerApplyIgnoresOutOfOrderStateRegression(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = store.Apply(context.Background(), testCasdoorEvent("evt-new", "delete-user", 20, true), time.Hour)
	require.NoError(t, err)
	stale, err := store.Apply(context.Background(), testCasdoorEvent("evt-old", "logout", 10, false), time.Hour)
	require.NoError(t, err)
	require.False(t, stale.Applied)

	state, err := store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.Generation)
	require.True(t, state.Blocked)
	require.Equal(t, int64(20), state.EventOrder)
}

func TestBadgerConfirmActiveFailsAfterConcurrentGenerationAdvance(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	_, err = store.Apply(context.Background(), testCasdoorEvent("evt-delete", "delete-user", 1, true), time.Hour)
	require.NoError(t, err)
	observed, err := store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err)
	_, err = store.Apply(context.Background(), testCasdoorEvent("evt-logout", "logout", 2, false), time.Hour)
	require.NoError(t, err)

	_, err = store.ConfirmActive(context.Background(), testIdentityKey(), observed.Generation)
	require.ErrorIs(t, err, ErrGenerationChanged)
	state, err := store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err)
	require.True(t, state.Blocked)
}

func TestBadgerPendingHooksSeekPastStateRecords(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	_, err = store.Apply(context.Background(), testCasdoorEvent("evt-state", "logout", 1, false), time.Hour)
	require.NoError(t, err)
	hook := PendingHook{ID: "hook-1", Event: testCasdoorEvent("evt-hook", "delete-user", 2, true), NextAttempt: time.Now().UTC()}
	require.NoError(t, store.SavePendingHook(context.Background(), hook))

	pending, err := store.PendingHooks(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, hook.ID, pending[0].ID)
}

func TestBadgerRejectsSameEventIDWithDifferentPayload(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	event := testCasdoorEvent("evt-1", "logout", 1, false)
	_, err = store.Apply(context.Background(), event, time.Hour)
	require.NoError(t, err)
	event.EventType = "delete-user"
	event.Blocked = true

	_, err = store.Apply(context.Background(), event, time.Hour)
	require.ErrorIs(t, err, ErrInvalidEvent)
}

func TestBadgerConcurrentApplyAdvancesGenerationOnce(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	event := testCasdoorEvent("evt-concurrent", "logout", 1, false)

	const workers = 64
	start := make(chan struct{})
	results := make(chan ApplyResult, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, err := store.Apply(context.Background(), event, time.Hour)
			results <- result
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	applied := 0
	for result := range results {
		if result.Applied {
			applied++
		}
		require.Equal(t, uint64(1), result.Generation)
	}
	require.Equal(t, 1, applied)
}

func TestBadgerPendingHooksAppliesLimitAfterGlobalOrdering(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	now := time.Now().UTC()
	require.NoError(t, store.SavePendingHook(context.Background(), PendingHook{ID: "a-late", NextAttempt: now.Add(time.Hour)}))
	require.NoError(t, store.SavePendingHook(context.Background(), PendingHook{ID: "z-early", NextAttempt: now}))

	pending, err := store.PendingHooks(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "z-early", pending[0].ID)
}

func TestBadgerSnapshotNeverRollsBackGeneration(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	key := testIdentityKey()
	require.NoError(t, store.SaveSnapshot(context.Background(), State{Key: key, Generation: 5, Blocked: true, EventOrder: 20}))

	require.NoError(t, store.SaveSnapshot(context.Background(), State{Key: key, Generation: 4, Blocked: false, EventOrder: 30}))
	state, err := store.Current(context.Background(), key)
	require.NoError(t, err)
	require.Equal(t, uint64(5), state.Generation)
	require.True(t, state.Blocked)
	require.Equal(t, int64(20), state.EventOrder)
}

func testIdentityKey() IdentityKey {
	return IdentityKey{Service: "shop", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor, Subject: "alice"}
}

func testCasdoorEvent(id, eventType string, order int64, blocked bool) types.CasdoorEvent {
	return types.CasdoorEvent{
		ID:              id,
		ServiceName:     "shop",
		AuthType:        types.AuthTypeUser,
		Provider:        types.AuthProviderCasdoor,
		ProviderSubject: "alice",
		UID:             "user-1",
		EventType:       eventType,
		EventOrder:      order,
		Blocked:         blocked,
		OccurredAt:      time.Unix(1_900_000_000+order, 0).UTC(),
	}
}
