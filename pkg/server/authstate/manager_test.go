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
func (unavailableStore) MarkPendingHookReady(context.Context, string) error {
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
	publish          func(context.Context, event.PublishRequest) error
}

func (f *fakeAuthEventBridge) Subscribe(_ string, handler event.Handler) (func(), error) {
	f.handler = handler
	return func() { f.localCanceled = true }, nil
}

func (f *fakeAuthEventBridge) SubscribeExternal(_ context.Context, subject string) (func(), error) {
	f.subject = subject
	return func() { f.externalCanceled = true }, nil
}

func (f *fakeAuthEventBridge) Publish(ctx context.Context, request event.PublishRequest) error {
	if f.publish != nil {
		return f.publish(ctx, request)
	}
	return nil
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

func TestSharedManagerSignalsWebSocketClosureWhenAuthorityFails(t *testing.T) {
	published := make(chan event.PublishRequest, 1)
	bridge := &fakeAuthEventBridge{publish: func(_ context.Context, request event.PublishRequest) error {
		published <- request
		return nil
	}}
	manager := newManagerWithStores("shop", unavailableStore{}, unavailableStore{}, true)
	require.NoError(t, manager.bindEventBridge(bridge))
	t.Cleanup(func() { require.NoError(t, manager.Close()) })

	err := manager.Authorize(context.Background(), testAuthIdentity(1))

	require.ErrorIs(t, err, ErrAuthorityUnavailable)
	request := <-published
	require.Equal(t, event.ControlDelivery, request.Class)
	require.False(t, request.External)
	require.Equal(t, types.CasdoorAuthorityUnavailableEventType, request.Envelope.Type)
	require.Equal(t, "shop", request.Envelope.Source)
}

func TestManagerProcessEventWaitsForControlAndDoesNotRepublishCompleteDuplicate(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	started := make(chan struct{})
	release := make(chan struct{})
	publishCalls := 0
	bridge := &fakeAuthEventBridge{publish: func(_ context.Context, request event.PublishRequest) error {
		publishCalls++
		require.Equal(t, event.ControlDelivery, request.Class)
		require.Equal(t, IdentityChangedEventType, request.Envelope.Type)
		close(started)
		<-release
		return nil
	}}
	manager := newManagerWithStores("shop", store, store, false)
	require.NoError(t, manager.bindEventBridge(bridge))
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	eventValue := testCasdoorEvent("evt-control", "logout", 1, false)
	done := make(chan error, 1)
	go func() {
		_, processErr := manager.ProcessEvent(context.Background(), eventValue, time.Hour)
		done <- processErr
	}()

	<-started
	select {
	case <-done:
		t.Fatal("控制事件完成前ProcessEvent不得返回")
	default:
	}
	state, err := store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.Generation, "控制投递等待期间权威世代应已持久化")
	close(release)
	require.NoError(t, <-done)

	result, err := manager.ProcessEvent(context.Background(), eventValue, time.Hour)
	require.NoError(t, err)
	require.True(t, result.ControlPublished)
	require.Equal(t, 1, publishCalls)
	state, err = store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err)
	require.Equal(t, uint64(1), state.Generation)
}

func TestManagerPendingHookRetriesWithoutReapplyingGeneration(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	manager := newManagerWithStores("shop", store, store, false)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	now := time.Unix(1_900_000_000, 0).UTC()
	eventValue := testCasdoorEvent("evt-hook", "logout", 1, false)
	eventValue.Generation = 1
	require.NoError(t, manager.SavePendingHook(context.Background(), PendingHook{
		ID: eventValue.ID, Event: eventValue, Ready: true, NextAttempt: now,
	}))
	calls := 0
	manager.hook = casdoorEventHookFunc(func(context.Context, types.CasdoorEvent) error {
		calls++
		if calls == 1 {
			return errors.New("temporary business failure")
		}
		return nil
	})
	manager.hookBackoff = []time.Duration{time.Second}

	require.NoError(t, manager.processPendingHooks(context.Background(), now))
	pending, err := manager.PendingHooks(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, 1, pending[0].Attempts)
	require.Equal(t, now.Add(time.Second), pending[0].NextAttempt)

	require.NoError(t, manager.processPendingHooks(context.Background(), now.Add(time.Second)))
	pending, err = manager.PendingHooks(context.Background(), 10)
	require.NoError(t, err)
	require.Empty(t, pending)
	require.Equal(t, 2, calls)
	state, err := store.Current(context.Background(), testIdentityKey())
	require.NoError(t, err)
	require.Zero(t, state.Generation, "Hook重试不得推进撤销世代")
}

func TestManagerHookTimeoutKeepsExecutionBounded(t *testing.T) {
	store, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	manager := newManagerWithStores("shop", store, store, false)
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	manager.hookTimeout = 10 * time.Millisecond
	manager.hookBackoff = []time.Duration{time.Second}
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	manager.hook = casdoorEventHookFunc(func(context.Context, types.CasdoorEvent) error {
		<-release
		return nil
	})
	now := time.Unix(1_900_000_000, 0).UTC()
	require.NoError(t, manager.SavePendingHook(context.Background(), PendingHook{
		ID: "blocked-hook", Event: testCasdoorEvent("blocked-hook", "logout", 1, false), Ready: true, NextAttempt: now,
	}))

	require.NoError(t, manager.processPendingHooks(context.Background(), now))
	require.Len(t, manager.hookSlots, 1, "超时Hook必须继续占用唯一执行槽，禁止派生更多执行")
	require.NoError(t, manager.processPendingHooks(context.Background(), now.Add(time.Second)))
	require.Len(t, manager.hookSlots, 1)
	pending, err := manager.PendingHooks(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, 2, pending[0].Attempts)
}

type casdoorEventHookFunc func(context.Context, types.CasdoorEvent) error

func (f casdoorEventHookFunc) OnCasdoorEvent(ctx context.Context, event types.CasdoorEvent) error {
	return f(ctx, event)
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

func TestIdentityKeyUsesSignedAuthorityService(t *testing.T) {
	identity := types.AuthIdentity{
		AuthType:         types.AuthTypeManage,
		Provider:         types.AuthProviderCasdoor,
		ProviderSubject:  "alice",
		AuthorityService: " Orders ",
	}
	require.Equal(t, "orders", identityKey("users", identity).Service)

	identity.AuthorityService = ""
	require.Equal(t, "users", identityKey("users", identity).Service)
}
