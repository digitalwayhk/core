// 本文件验证通知健康只保护长连接，普通请求仍直接查询权威，且检查容量有界。
package authstate

import (
	"context"
	"testing"

	"github.com/digitalwayhk/core/internal/controlnotify"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type notificationAuthTestBridge struct {
	fakeAuthEventBridge
	epoch uint64
	ready bool
}

func (b *notificationAuthTestBridge) NotificationState() (uint64, bool) { return b.epoch, b.ready }

func TestNotificationFailureDoesNotReplaceHTTPAuthority(t *testing.T) {
	authority, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	snapshot, err := OpenBadgerStore(t.TempDir())
	require.NoError(t, err)
	m := newManagerWithStores("shop", authority, snapshot, true)
	t.Cleanup(func() { require.NoError(t, m.Close()) })
	b := &notificationAuthTestBridge{epoch: 1}
	require.NoError(t, m.bindEventBridge(b))
	id := testAuthIdentity(0)
	require.NoError(t, m.Authorize(context.Background(), id), "HTTP 不依赖通知通道")
	s := &controlnotify.AuthSessionState{}
	ctx := controlnotify.WithAuthSession(context.Background(), s)
	require.ErrorIs(t, m.Authorize(ctx, id), ErrAuthorityUnavailable)
	b.ready = true
	require.NoError(t, m.Authorize(ctx, id))
	b.epoch++
	require.ErrorIs(t, m.Authorize(ctx, id), ErrAuthorityUnavailable, "旧连接不能跨越断线代次")
	require.NoError(t, m.Authorize(context.Background(), id))
	for i := 0; i < cap(m.notificationSlots); i++ {
		m.notificationSlots <- struct{}{}
	}
	fresh := controlnotify.WithAuthSession(context.Background(), &controlnotify.AuthSessionState{})
	require.ErrorIs(t, m.Authorize(fresh, id), ErrAuthorityUnavailable, "容量耗尽不能排无界等待队列")
	require.NoError(t, m.Authorize(context.Background(), id))
	other := types.AuthIdentity{}
	require.NoError(t, m.Authorize(fresh, other), "其他认证域不受 Casdoor 通知影响")
	for len(m.notificationSlots) > 0 {
		<-m.notificationSlots
	}
}
