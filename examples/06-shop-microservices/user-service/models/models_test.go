// 本文件验证当前服务模型层的持久化、投影和幂等边界。
package models

import (
	"errors"
	"os"
	"sync/atomic"
	"testing"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain 验证当前场景的业务闭环和边界行为。
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

// TestInboxRunsDuplicateControlEventOnce 验证当前场景的业务闭环和边界行为。
func TestInboxRunsDuplicateControlEventOnce(t *testing.T) {
	var calls atomic.Int32
	operation := func() error { calls.Add(1); return nil }
	require.NoError(t, ProcessInbox("trace-event-once", "event-once", "shop.order.changed", operation))
	require.NoError(t, ProcessInbox("trace-event-once", "event-once", "shop.order.changed", operation))
	assert.Equal(t, int32(1), calls.Load())
	var items []*Inbox
	require.NoError(t, RunTransaction(func(action persistencetypes.IDataAction) error {
		query := &persistencetypes.SearchItem{Model: NewInbox(), Size: 1}
		query.AddWhereN("EventID", "event-once")
		return action.Load(query, &items)
	}))
	require.Len(t, items, 1)
	require.Equal(t, "trace-event-once", items[0].TraceID)
}

// TestInboxRetriesUnprocessedEvent 验证当前场景的业务闭环和边界行为。
func TestInboxRetriesUnprocessedEvent(t *testing.T) {
	var calls atomic.Int32
	operation := func() error {
		if calls.Add(1) == 1 {
			return errors.New("temporary notification failure")
		}
		return nil
	}
	require.Error(t, ProcessInbox("trace-event-retry", "event-retry", "shop.order.changed", operation))
	require.NoError(t, ProcessInbox("trace-event-retry", "event-retry", "shop.order.changed", operation))
	require.NoError(t, ProcessInbox("trace-event-retry", "event-retry", "shop.order.changed", operation))
	assert.Equal(t, int32(2), calls.Load())
}

// TestAddressOwnershipUsesTrustedUser 验证当前场景的业务闭环和边界行为。
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

// TestEnsureUserMapsAuthIdentityToStableNumericID 验证当前场景的业务闭环和边界行为。
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
