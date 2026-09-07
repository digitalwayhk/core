// 本文件验证生产 ServiceContext 接线、真实 Broker 广播和真实 WebSocket 保护的完整路径。
package melody

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/internal/controlnotify"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type notificationCompositionService struct{ name string }

func (s *notificationCompositionService) ServiceName() string    { return s.name }
func (*notificationCompositionService) Routers() []types.IRouter { return nil }

func TestNotificationProductionCompositionClosesRealSocketOnInvalidFrame(t *testing.T) {
	for _, provider := range []string{"redis-stream", "nats-jetstream"} {
		t.Run(provider, func(t *testing.T) {
			addr := os.Getenv("CORE_TEST_REDIS_ADDR")
			natsURL := os.Getenv("CORE_TEST_NATS_URL")
			if addr == "" || provider == "nats-jetstream" && natsURL == "" {
				t.Skip("NOT RUN: 真实 Broker 未配置")
			}
			name := fmt.Sprintf("notify-composition-%d", time.Now().UnixNano())
			cfg := config.NewServiceDefaultConfig(name, 0)
			cfg.Cluster.Mode = "off"
			cfg.Transport.Internal = ""
			cfg.Transport.Fallback = nil
			cfg.MQ.Mode = "on"
			cfg.MQ.Provider = provider
			cfg.MQ.Usage = []string{"event-stream"}
			cfg.MQ.RedisStream.Addr = addr
			cfg.MQ.RedisStream.Prefix = name
			cfg.MQ.NATSJetStream.URL = natsURL
			cfg.MQ.NATSJetStream.StreamPrefix = name
			cfg.Auth.AccessSecret = "notification-composition-test-secret"
			cfg.Auth.CasDoor.Enable = true
			cfg.Auth.CasDoor.WebhookSecret = "test-webhook-secret"
			key, err := rsa.GenerateKey(rand.Reader, 2048)
			require.NoError(t, err)
			der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
			require.NoError(t, err)
			certificate := strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
			yaml := "certificate: |\n  " + strings.ReplaceAll(certificate, "\n", "\n  ") + "\nserver:\n  endpoint: http://127.0.0.1:18000\n  client_id: notification-test\n  client_secret: test-only\n  organization: test\n  application: test\n  frontend_url: http://localhost:3000\n"
			cfg.Auth.CasDoor.YamlFilePath = filepath.Join(t.TempDir(), "casdoor.yaml")
			require.NoError(t, os.WriteFile(cfg.Auth.CasDoor.YamlFilePath, []byte(yaml), 0600))
			cfg.AuthRevocation = config.AuthRevocationConfig{Mode: config.AuthRevocationModeShared, BadgerPath: t.TempDir(), Redis: config.AuthRevocationRedisConfig{Addr: addr, Prefix: name}}
			sc := router.NewServiceContextWithConfig(&notificationCompositionService{name: name}, cfg)
			defer sc.SetRunState(false)
			manager := NewMelodyManager(sc)
			defer manager.Close()
			server := httptest.NewServer(http.HandlerFunc(manager.ServeWS))
			defer server.Close()
			conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer conn.Close()
			id := types.AuthIdentity{UID: "user-1", Username: "用户一", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor, ProviderSubject: "alice", AuthorityService: name}
			pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{Claims: safe.NewClaims(id.UID, id.Username), Identity: id, AuthType: types.AuthTypeUser, IssuedAt: time.Now().UTC(), AccessSecret: cfg.Auth.AccessSecret, AccessExpireSeconds: 60})
			require.NoError(t, err)
			require.NoError(t, conn.WriteJSON(map[string]any{"event": "sub", "channel": "logon", "data": map[string]string{"token": pair.AccessToken}}))
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
			for {
				var response Message
				require.NoError(t, conn.ReadJSON(&response))
				if response.Channel == "logon" {
					require.Equal(t, "success", response.Event)
					break
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			sender, err := controlnotify.Open(ctx, cfg.MQ, name, "identity")
			require.NoError(t, err)
			defer sender.Close()
			require.NoError(t, sender.Publish(ctx, []byte("invalid-frame")))
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(6*time.Second)))
			_, _, err = conn.ReadMessage()
			require.Error(t, err)
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("生产通知接线未关闭会话")
			}
			require.NoError(t, sc.AuthRevocationManager.Authorize(context.Background(), id), "HTTP 权威仍可用")
		})
	}
}
