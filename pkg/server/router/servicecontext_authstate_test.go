package router

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/require"
)

func TestServiceContextOwnsAuthLifecycleComponents(t *testing.T) {
	name := fmt.Sprintf("auth-lifecycle-%d", time.Now().UnixNano())
	service := &authHookTestService{name: name}
	cfg := localCasdoorServiceConfig(t, name)

	sc := NewServiceContextWithConfig(service, cfg)
	require.NotNil(t, sc.CasdoorClients)
	require.NotNil(t, sc.CasdoorClients.Auth())
	require.NotNil(t, sc.AuthRevocationManager)
	require.Same(t, service, sc.AuthHookProvider)
	require.Same(t, service, sc.AuthRequestHookProvider)
	require.Same(t, service, sc.CasdoorEventHookProvider)
	require.Equal(t, 1, sc.EventStream.SubscriberCount(authstate.IdentityChangedEventType))

	sc.SetRunState(true)
	sc.SetRunState(false)
	require.Nil(t, sc.CasdoorClients)
	require.Nil(t, sc.AuthRevocationManager)
	require.Nil(t, sc.AuthRequestHookProvider)
	require.Nil(t, sc.CasdoorEventHookProvider)

	reopened, err := authstate.OpenBadgerStore(cfg.AuthRevocation.BadgerPath)
	require.NoError(t, err, "ServiceContext关闭完成后必须释放Badger锁")
	require.NoError(t, reopened.Close())
}

func TestServiceContextAuthComponentsAreIsolatedAcrossServices(t *testing.T) {
	firstName := fmt.Sprintf("auth-isolation-a-%d", time.Now().UnixNano())
	secondName := fmt.Sprintf("auth-isolation-b-%d", time.Now().UnixNano())
	first := NewServiceContextWithConfig(&authHookTestService{name: firstName}, localCasdoorServiceConfig(t, firstName))
	second := NewServiceContextWithConfig(&authHookTestService{name: secondName}, localCasdoorServiceConfig(t, secondName))
	first.SetRunState(true)
	second.SetRunState(true)
	t.Cleanup(func() {
		first.SetRunState(false)
		second.SetRunState(false)
	})

	require.NotSame(t, first.CasdoorClients, second.CasdoorClients)
	require.NotSame(t, first.AuthRevocationManager, second.AuthRevocationManager)
}

func TestServiceContextCanCloseAuthLifecycleBeforeStart(t *testing.T) {
	name := fmt.Sprintf("auth-close-before-start-%d", time.Now().UnixNano())
	cfg := localCasdoorServiceConfig(t, name)
	sc := NewServiceContextWithConfig(&authHookTestService{name: name}, cfg)

	sc.SetRunState(false)
	require.Nil(t, sc.AuthRevocationManager)
	require.Nil(t, sc.ServiceEventBridge)
	reopened, err := authstate.OpenBadgerStore(cfg.AuthRevocation.BadgerPath)
	require.NoError(t, err)
	require.NoError(t, reopened.Close())
}

func TestSharedAuthRequiresExternalEventBridgeAndCleansFailedInitialization(t *testing.T) {
	name := fmt.Sprintf("auth-shared-no-bridge-%d", time.Now().UnixNano())
	cfg := localCasdoorServiceConfig(t, name)
	cfg.AuthRevocation.Mode = config.AuthRevocationModeShared
	cfg.AuthRevocation.Redis.Addr = "127.0.0.1:6379"

	require.PanicsWithValue(t, "auth lifecycle: shared mode requires MQ event-stream", func() {
		NewServiceContextWithConfig(&authHookTestService{name: name}, cfg)
	})

	reopened, err := authstate.OpenBadgerStore(cfg.AuthRevocation.BadgerPath)
	require.NoError(t, err, "初始化失败必须释放本次创建的Badger")
	require.NoError(t, reopened.Close())
}

func localCasdoorServiceConfig(t *testing.T, name string) *config.ServerConfig {
	t.Helper()
	cfg := config.NewServiceDefaultConfig(name, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil
	cfg.Auth.CasDoor.Enable = true
	cfg.Auth.CasDoor.WebhookSecret = "webhook-" + name
	cfg.Auth.CasDoor.YamlFilePath = writeRouterCasdoorConfig(t, name)
	cfg.AuthRevocation.BadgerPath = filepath.Join(t.TempDir(), "auth-state")
	return cfg
}

func writeRouterCasdoorConfig(t *testing.T, name string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER}))
	path := filepath.Join(t.TempDir(), "casdoor.yaml")
	content := "certificate: |\n"
	for _, line := range splitNonEmptyLines(certificate) {
		content += "  " + line + "\n"
	}
	content += "server:\n" +
		"  endpoint: http://127.0.0.1:18000\n" +
		"  client_id: client-" + name + "\n" +
		"  client_secret: secret-" + name + "\n" +
		"  organization: org-" + name + "\n" +
		"  application: app-" + name + "\n" +
		"  frontend_url: http://localhost:3000\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func splitNonEmptyLines(value string) []string {
	result := make([]string, 0)
	start := 0
	for index, current := range value {
		if current != '\n' {
			continue
		}
		if index > start {
			result = append(result, value[start:index])
		}
		start = index + 1
	}
	return result
}
