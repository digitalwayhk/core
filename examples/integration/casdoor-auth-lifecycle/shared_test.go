package casdoorauthlifecycle_test

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestSharedRedisAuthority(t *testing.T) {
	if os.Getenv("CORE_TEST_CASDOOR_AUTH") != "1" {
		t.Skip("设置 CORE_TEST_CASDOOR_AUTH=1 后运行显式 Redis 集成测试")
	}
	addr := strings.TrimSpace(os.Getenv("CORE_TEST_REDIS_ADDR"))
	if addr == "" {
		t.Fatal("CORE_TEST_REDIS_ADDR is required")
	}
	proxy := newRedisCutoffProxy(t, addr)
	cfg := config.AuthRevocationConfig{
		Mode:       config.AuthRevocationModeShared,
		BadgerPath: t.TempDir(),
		Redis:      config.AuthRevocationRedisConfig{Addr: proxy.Addr(), Prefix: "core:test:casdoor:" + time.Now().Format("20060102150405.000000000")},
	}
	manager, err := authstate.NewManager("shared-integration", cfg, authstate.WithEventBridge(noopSharedBridge{}))
	require.NoError(t, err)
	defer manager.Close()

	identity := types.AuthIdentity{UID: "redis-alice", AuthType: types.AuthTypeUser, Provider: types.AuthProviderCasdoor, ProviderSubject: "alice"}
	state, err := manager.Current(context.Background(), identity)
	require.NoError(t, err)
	require.False(t, state.Blocked)
	identity.Generation = state.Generation
	now := time.Now().UTC()
	result, err := manager.ProcessEvent(context.Background(), types.CasdoorEvent{
		ID: "shared-event", ServiceName: "shared-integration", AuthType: identity.AuthType,
		Provider: identity.Provider, ProviderSubject: identity.ProviderSubject, UID: identity.UID,
		EventType: "logout", EventOrder: now.UnixNano(), Blocked: true, OccurredAt: now,
	}, time.Minute)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Greater(t, result.Generation, uint64(0))
	current, err := manager.Current(context.Background(), identity)
	require.NoError(t, err)
	require.True(t, current.Blocked)
	require.Equal(t, result.Generation, current.Generation)

	proxy.Cut()
	failedCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = manager.Authorize(failedCtx, identity)
	require.ErrorIs(t, err, authstate.ErrAuthorityUnavailable, "共享 Redis 连接中断后不得使用 Badger 快照授权")
}

type noopSharedBridge struct{}

func (noopSharedBridge) Subscribe(string, event.Handler) (func(), error) {
	return func() {}, nil
}

func (noopSharedBridge) SubscribeExternal(context.Context, string) (func(), error) {
	return func() {}, nil
}

func (noopSharedBridge) Publish(context.Context, event.PublishRequest) error { return nil }

type redisCutoffProxy struct {
	listener net.Listener
	target   string
	mu       sync.Mutex
	closed   bool
	conns    map[net.Conn]struct{}
}

func newRedisCutoffProxy(t *testing.T, target string) *redisCutoffProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	proxy := &redisCutoffProxy{listener: listener, target: target, conns: make(map[net.Conn]struct{})}
	go proxy.accept()
	t.Cleanup(proxy.Cut)
	return proxy
}

func (p *redisCutoffProxy) Addr() string { return p.listener.Addr().String() }

func (p *redisCutoffProxy) accept() {
	for {
		client, err := p.listener.Accept()
		if err != nil {
			return
		}
		upstream, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.track(client, upstream)
		go p.pipe(client, upstream)
		go p.pipe(upstream, client)
	}
}

func (p *redisCutoffProxy) track(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		for _, conn := range conns {
			_ = conn.Close()
		}
		return
	}
	for _, conn := range conns {
		p.conns[conn] = struct{}{}
	}
}

func (p *redisCutoffProxy) pipe(dst, src net.Conn) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}

func (p *redisCutoffProxy) Cut() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	_ = p.listener.Close()
	for conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = nil
	p.mu.Unlock()
}
