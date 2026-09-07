// 本文件使用真实 WebSocket 和 Redis 撤销权威，故障注入通知状态以覆盖无订阅的登录会话。
package melody

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/internal/controlnotify"
	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type sessionNotificationTestBridge struct {
	*event.ServiceEventBridge
	down    atomic.Bool
	epoch   atomic.Uint64
	stalled atomic.Bool
	release chan struct{}
}

func TestNotificationOldWatchdogCannotExpireReloggedSession(t *testing.T) {
	manager, err := authstate.NewManager("relogin", config.AuthRevocationConfig{Mode: config.AuthRevocationModeLocal, BadgerPath: t.TempDir()})
	require.NoError(t, err)
	defer manager.Close()
	s := &SessionSubscriptions{sessionContext: context.Background()}
	id := &safe.AccessTokenIdentity{Identity: types.AuthIdentity{}}
	first := &controlnotify.AuthSessionState{}
	require.True(t, first.Accept(1, true))
	s.startNotificationSessionLocked(manager, id, first)
	old := s.notificationGen.Load()
	second := &controlnotify.AuthSessionState{}
	require.True(t, second.Accept(2, true))
	s.startNotificationSessionLocked(manager, id, second)
	defer s.stopNotificationSessionLocked()
	current := s.notificationGen.Load()
	s.expireNotificationSession(old)
	require.Equal(t, current, s.notificationGen.Load())
	require.False(t, s.notificationExpired.Load())
	require.Same(t, second, s.notificationState)
}

func (b *sessionNotificationTestBridge) NotificationState() (uint64, bool) {
	if b.stalled.Load() {
		<-b.release
	}
	return b.epoch.Load(), !b.down.Load()
}
func (b *sessionNotificationTestBridge) SubscribeExternal(context.Context, string) (func(), error) {
	return func() {}, nil
}

func TestNotificationProtectsIdleRealWebSocketSession(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: real Redis authority not configured")
	}
	for _, scenario := range []string{"reject-logon", "missing-revocation", "reconnected-gap", "stalled-check"} {
		t.Run(scenario, func(t *testing.T) {
			bridge := &sessionNotificationTestBridge{ServiceEventBridge: event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})}
			bridge.epoch.Store(1)
			bridge.release = make(chan struct{})
			defer bridge.Close(context.Background())
			cfg := config.NewServiceDefaultConfig("notifyws", 0)
			cfg.Auth.AccessSecret = "internal-notification-test-access-secret"
			cfg.AuthRevocation = config.AuthRevocationConfig{Mode: config.AuthRevocationModeShared, BadgerPath: t.TempDir(), Redis: config.AuthRevocationRedisConfig{Addr: addr, Prefix: fmt.Sprintf("notify-auth-test-%d", time.Now().UnixNano())}}
			manager, err := authstate.NewManager("notifyws", cfg.AuthRevocation, authstate.WithEventBridge(bridge))
			require.NoError(t, err)
			defer manager.Close()
			sc := &router.ServiceContext{Config: cfg, Service: &types.Service{Name: "notifyws"}, AuthRevocationManager: manager}
			sc.Router = &router.ServiceRouter{Service: sc}
			wsManager := NewMelodyManager(sc)
			defer wsManager.Close()
			server := httptest.NewServer(http.HandlerFunc(wsManager.ServeWS))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.Close()
			identity := types.AuthIdentity{UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor, ProviderSubject: "alice", AuthorityService: "notifyws"}
			pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{Claims: safe.NewClaims(identity.UID, identity.Username), Identity: identity, AuthType: types.AuthTypeUser, IssuedAt: time.Now().UTC(), AccessSecret: cfg.Auth.AccessSecret, AccessExpireSeconds: 60})
			require.NoError(t, err)
			if scenario == "reject-logon" {
				bridge.down.Store(true)
			}
			require.NoError(t, conn.WriteJSON(map[string]any{"event": "sub", "channel": "logon", "data": map[string]string{"token": pair.AccessToken}}))
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
			var response Message
			for {
				require.NoError(t, conn.ReadJSON(&response))
				if response.Channel == "logon" {
					break
				}
			}
			if scenario == "reject-logon" {
				require.Equal(t, "error", response.Event)
				return
			}
			require.Equal(t, "success", response.Event)
			if scenario == "missing-revocation" {
				_, err = manager.ApplyEvent(context.Background(), types.CasdoorEvent{ID: "lost-notification", ServiceName: "notifyws", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor, ProviderSubject: "alice", UID: "user-1", EventType: "logout", EventOrder: 1, Blocked: true, OccurredAt: time.Now().UTC()}, time.Hour)
				require.NoError(t, err)
			} else if scenario == "stalled-check" {
				bridge.stalled.Store(true)
				defer close(bridge.release)
			} else {
				bridge.epoch.Add(2)
			}
			// 没有订阅任何路由；必须由会话层主动关闭，不能靠后续 API 访问发现。
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(7*time.Second)))
			_, _, err = conn.ReadMessage()
			require.Error(t, err)
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("idle authenticated socket survived notification failure")
			}
		})
	}
}
