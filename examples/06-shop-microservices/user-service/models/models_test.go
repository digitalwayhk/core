package models

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "shop-user-test-")
	if err != nil {
		panic(err)
	}
	utils.TESTPATH = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestInboxRunsDuplicateControlEventOnce(t *testing.T) {
	var calls atomic.Int32
	operation := func() error { calls.Add(1); return nil }
	require.NoError(t, ProcessInbox("event-once", "shop.order.changed", operation))
	require.NoError(t, ProcessInbox("event-once", "shop.order.changed", operation))
	assert.Equal(t, int32(1), calls.Load())
}

func TestInboxRetriesUnprocessedEvent(t *testing.T) {
	var calls atomic.Int32
	operation := func() error {
		if calls.Add(1) == 1 {
			return errors.New("temporary notification failure")
		}
		return nil
	}
	require.Error(t, ProcessInbox("event-retry", "shop.order.changed", operation))
	require.NoError(t, ProcessInbox("event-retry", "shop.order.changed", operation))
	require.NoError(t, ProcessInbox("event-retry", "shop.order.changed", operation))
	assert.Equal(t, int32(2), calls.Load())
}

func TestAddressOwnershipUsesTrustedUser(t *testing.T) {
	user, err := EnsureUser("buyer-a", "用户 A")
	require.NoError(t, err)
	item := NewAddress()
	item.SetID(1001)
	item.UserID = user.ID
	item.Recipient = "用户 A"
	item.Detail = "1 号"
	require.NoError(t, InsertAddress(item))

	owned, err := FindOwnedAddress(user.ID, item.ID)
	require.NoError(t, err)
	assert.Equal(t, "1 号", owned.Detail)
	foreign, err := FindOwnedAddress(user.ID+1, item.ID)
	assert.Nil(t, foreign)
	assert.NoError(t, err)
}

func TestEnsureUserMapsAuthIdentityToStableNumericID(t *testing.T) {
	first, err := EnsureUser("auth-buyer-1", "Buyer")
	require.NoError(t, err)
	second, err := EnsureUser("auth-buyer-1", "Buyer")
	require.NoError(t, err)
	require.NotZero(t, first.ID)
	require.Equal(t, first.ID, second.ID)
	require.True(t, first.Enabled)
	require.Equal(t, "auth-buyer-1", first.AuthUserID)
}
