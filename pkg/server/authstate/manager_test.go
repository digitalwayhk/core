package authstate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
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

type fakeAuthEventBridge struct {
	localCanceled    bool
	externalCanceled bool
	handler          event.Handler
	subject          string
}

func (f *fakeAuthEventBridge) Subscribe(_ string, handler event.Handler) (func(), error) {
	f.handler = handler
	return func() { f.localCanceled = true }, nil
}

func (f *fakeAuthEventBridge) SubscribeExternal(_ context.Context, subject string) (func(), error) {
	f.subject = subject
	return func() { f.externalCanceled = true }, nil
}

func TestManagerBeginCloseStopsSubscriptionsBeforeStorageClose(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	bridge := &fakeAuthEventBridge{}
	manager := newManagerWithStores("shop", store, store, false)
	require.NoError(t, manager.bindEventBridge(bridge))

	manager.BeginClose()
	require.True(t, bridge.localCanceled)
	require.False(t, bridge.externalCanceled)
	require.ErrorIs(t, manager.Authorize(context.Background(), testAuthIdentity(0)), ErrAuthorityUnavailable)
	_, err = store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err, "BeginClose只停认证运行时，存储留到最后关闭")
	require.NoError(t, manager.Close())
}

func TestSharedManagerBindsAndClosesLocalAndExternalSubscriptions(t *testing.T) {
	snapshot, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	bridge := &fakeAuthEventBridge{}
	manager := newManagerWithStores("shop", unavailableStore{}, snapshot, true)
	require.NoError(t, manager.bindEventBridge(bridge))
	require.Equal(t, IdentityChangedSubject("shop"), bridge.subject)

	manager.BeginClose()
	require.True(t, bridge.localCanceled)
	require.True(t, bridge.externalCanceled)
	require.NoError(t, manager.Close())
}

func TestManagerEventSubscriptionStoresConfirmedSnapshot(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	bridge := &fakeAuthEventBridge{}
	manager := newManagerWithStores("shop", store, store, false)
	require.NoError(t, manager.bindEventBridge(bridge))
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	payload := testCasdoorEvent("evt-control", "delete-user", 9, true)
	payload.Generation = 4
	data, err := json.Marshal(payload)
	require.NoError(t, err)

	bridge.handler(&event.Envelope{Type: IdentityChangedEventType, Data: data})
	state, err := store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err)
	require.Equal(t, uint64(4), state.Generation)
	require.True(t, state.Blocked)
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
