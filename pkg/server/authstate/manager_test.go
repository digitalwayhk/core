package authstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestNewManagerRejectsUnknownModeAndEmptyService(t *testing.T) {
	cfg := config.AuthRevocationConfig{Mode: "fallback", BadgerPath: t.TempDir()}
	_, err := NewManager("shop", cfg)
	require.Error(t, err)

	cfg.Mode = config.AuthRevocationModeLocal
	_, err = NewManager(" ", cfg)
	require.Error(t, err)
}

type unavailableStore struct{}

func (unavailableStore) Current(context.Context, IdentityKey) (State, error) {
	return State{}, errors.New("backend detail")
}
func (unavailableStore) Apply(context.Context, types.CasdoorEvent, time.Duration) (ApplyResult, error) {
	return ApplyResult{}, errors.New("backend detail")
}
func (unavailableStore) ConfirmActive(context.Context, IdentityKey, uint64) (State, error) {
	return State{}, errors.New("backend detail")
}
func (unavailableStore) SaveSnapshot(context.Context, State) error {
	return errors.New("backend detail")
}
func (unavailableStore) MarkControlPublished(context.Context, types.CasdoorEvent) error {
	return errors.New("backend detail")
}
func (unavailableStore) SavePendingHook(context.Context, PendingHook) error {
	return errors.New("backend detail")
}
func (unavailableStore) PendingHooks(context.Context, int) ([]PendingHook, error) {
	return nil, errors.New("backend detail")
}
func (unavailableStore) AckHook(context.Context, string) error { return errors.New("backend detail") }
func (unavailableStore) Close() error                          { return nil }

func TestManagerAuthorizeFailsClosedWhenAuthorityUnavailable(t *testing.T) {
	manager := newManagerWithStores("shop", unavailableStore{}, nil, false)
	err := manager.Authorize(context.Background(), testAuthIdentity(0))
	require.ErrorIs(t, err, ErrAuthorityUnavailable)
	require.NotContains(t, err.Error(), "backend detail")
}

func TestSharedManagerNeverAuthorizesFromSnapshot(t *testing.T) {
	snapshot, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	identity := testAuthIdentity(0)
	require.NoError(t, snapshot.SaveSnapshot(context.Background(), State{Key: identityKey("shop", identity), Generation: 0}))
	manager := newManagerWithStores("shop", unavailableStore{}, snapshot, true)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	err = manager.Authorize(context.Background(), identity)
	require.ErrorIs(t, err, ErrAuthorityUnavailable)
}

func TestManagerAuthorizeRejectsBlockedOrStaleGeneration(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	manager := newManagerWithStores("shop", store, store, false)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	require.NoError(t, manager.Authorize(context.Background(), testAuthIdentity(0)))
	_, err = manager.ApplyEvent(context.Background(), testCasdoorEvent("evt-logout", "logout", 1, false), time.Hour)
	require.NoError(t, err)
	require.ErrorIs(t, manager.Authorize(context.Background(), testAuthIdentity(0)), ErrIdentityRevoked)
	require.NoError(t, manager.Authorize(context.Background(), testAuthIdentity(1)))
	_, err = manager.ApplyEvent(context.Background(), testCasdoorEvent("evt-delete", "delete-user", 2, true), time.Hour)
	require.NoError(t, err)
	require.ErrorIs(t, manager.Authorize(context.Background(), testAuthIdentity(2)), ErrIdentityRevoked)
}

func testAuthIdentity(generation uint64) types.AuthIdentity {
	return types.AuthIdentity{
		UID:             "user-1",
		AuthType:        types.AuthTypeUser,
		Provider:        types.AuthProviderCasdoor,
		ProviderSubject: "alice",
		Generation:      generation,
	}
}
