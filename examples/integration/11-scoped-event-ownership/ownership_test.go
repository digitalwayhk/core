// 本文件用两个真实处理进程、两个真实Gateway、MySQL与WS客户端验证非owner查询及免重订恢复。
package scopedownership_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/dto"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestScopedOwnershipRealGatewaysSQLAndWebSockets(t *testing.T) {
	addr, file := os.Getenv("CORE_TEST_REDIS_ADDR"), os.Getenv("CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE")
	if addr == "" || file == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR及CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE")
	}
	file, err := filepath.Abs(file)
	require.NoError(t, err)
	pki := integration.NewGRPCTestPKI(t, contract.ServiceName)
	identity := pki.Services[contract.ServiceName]
	prefix := fmt.Sprintf("core:example11:%d", time.Now().UnixNano())
	database := fmt.Sprintf("core_demo11_%d", time.Now().UnixNano())
	ctx := context.Background()
	p := mq.NewRedisStreamProvider(addr, prefix, 0)
	require.NoError(t, p.Connect(ctx))
	defer p.Close()
	m := contract.Manifest()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	require.NoError(t, p.BindOwnership(ctx, m))
	store, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	start := func(role string, index, capacity int) *integration.Suite {
		s, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/11-scoped-event-ownership/main", BinaryName: fmt.Sprintf("demo11-%s-%d", role, index), TempPrefix: "core-demo11-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: map[string]string{"CORE_DEMO11_CA_FILE": pki.CAFile, "CORE_DEMO11_CERT_FILE": identity.CertFile, "CORE_DEMO11_KEY_FILE": identity.KeyFile, "CORE_DEMO11_ROLE": role, "CORE_DEMO11_INDEX": strconv.Itoa(index), "CORE_DEMO11_REDIS_ADDR": addr, "CORE_DEMO11_PREFIX": prefix, "CORE_DEMO11_MYSQL_CONFIG_FILE": file, "CORE_DEMO11_DATABASE": database, "CORE_DEMO11_MAX_OWNED": strconv.Itoa(capacity)}})
		require.NoError(t, err)
		t.Cleanup(func() {
			if t.Failed() {
				s.PrintLog()
			}
			s.Stop()
		})
		return s
	}
	a := start("worker", 1, 1)
	var original ownership.Credential
	require.Eventually(t, func() bool { var err error; original, err = store.Resolve(ctx, m.Scope, "A"); return err == nil }, 3*time.Second, 10*time.Millisecond)
	b := start("worker", 2, 2)
	g1, g2 := start("gateway", 3, 1), start("gateway", 4, 1)
	tokens := map[*integration.Suite]map[string]string{}
	for _, s := range []*integration.Suite{a, b, g1, g2} {
		tokens[s] = map[string]string{"A": s.TokenFor(t, "A", 0), "B": s.TokenFor(t, "B", 0)}
	}
	query := func(s *integration.Suite, uid string) (dto.Snapshot, bool) {
		res, err := s.DoJSON(http.MethodPost, "/api/owned-demo/view", tokens[s][uid], map[string]string{"userID": "forged"})
		if err != nil || !res.Success {
			return dto.Snapshot{}, false
		}
		var value dto.Snapshot
		err = json.Unmarshal(res.Data, &value)
		return value, err == nil
	}
	denied := g1.RequestJSON(t, http.MethodPost, "/api/owned-demo/snapshot", "", map[string]string{"key": "A"})
	require.False(t, denied.Success, "外部HTTP不能冒充内部owner查询")
	denied = g1.RequestJSON(t, http.MethodPost, "/api/owned-demo/view", "", nil)
	require.False(t, denied.Success)
	var initial dto.Snapshot
	require.Eventually(t, func() bool { var ok bool; initial, ok = query(g1, "A"); return ok }, 5*time.Second, 20*time.Millisecond)
	require.Equal(t, original.InstanceID, initial.InstanceID)
	connect := func(s *integration.Suite, uid string) *websocket.Conn {
		conn, _, err := websocket.DefaultDialer.Dial(s.WebSocketURL, nil)
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		s.WriteWebSocket(t, conn, "sub", "logon", map[string]string{"token": tokens[s][uid]})
		require.Equal(t, "success", s.ReadWebSocket(t, conn, 3*time.Second).Event)
		s.WriteWebSocket(t, conn, "sub", "/api/owned-demo/view", map[string]string{"userID": "forged"})
		reply := s.ReadWebSocket(t, conn, 3*time.Second)
		require.Equal(t, "sub", reply.Event, string(reply.Data))
		return conn
	}
	anonymous, _, err := websocket.DefaultDialer.Dial(g1.WebSocketURL, nil)
	require.NoError(t, err)
	g1.WriteWebSocket(t, anonymous, "sub", "/api/owned-demo/view", nil)
	require.Equal(t, "error", g1.ReadWebSocket(t, anonymous, 3*time.Second).Event)
	anonymous.Close()
	c1, c2, cOther := connect(g1, "A"), connect(g2, "A"), connect(g2, "B")
	messages1, messages2, other := g1.StreamWebSocket(t, c1), g2.StreamWebSocket(t, c2), g2.StreamWebSocket(t, cOther)
	require.Eventually(t, func() bool {
		refs, e := store.References(ctx, m.Scope, "A")
		return e == nil && len(refs.References) == 2
	}, 3*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool { s, ok := query(g2, "A"); return ok && s.References == 2 }, 3*time.Second, 20*time.Millisecond)
	submit := func(s *integration.Suite, uid string, subject int) {
		r := s.RequestJSON(t, http.MethodPost, "/api/owned-demo/submit", tokens[s][uid], map[string]interface{}{"subject": subject, "userID": "forged"})
		require.True(t, r.Success, r.ErrorMessage)
	}
	waitFrame := func(messages <-chan integration.WebSocketMessage, count int64) dto.Snapshot {
		deadline := time.After(5 * time.Second)
		for {
			select {
			case message, ok := <-messages:
				require.True(t, ok)
				if message.Channel != "/api/owned-demo/view" || message.Event == "sub" || message.Event == "unsub" {
					continue
				}
				var state dto.Snapshot
				require.NoError(t, json.Unmarshal(message.Data, &state))
				require.Equal(t, "A", state.Key)
				if state.Count == count {
					return state
				}
			case <-deadline:
				t.Fatal("原客户端未收到持久状态通知")
				return dto.Snapshot{}
			}
		}
	}
	submit(g1, "A", 0)
	one := waitFrame(messages1, 1)
	two := waitFrame(messages2, 1)
	require.Equal(t, one.Epoch, two.Epoch)
	// 模拟一个Gateway的恢复租约丢失，保留真实WS连接及原订阅；等待原生15秒重建会话快照。
	activeRefs, err := store.References(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Len(t, activeRefs.References, 2)
	require.NoError(t, store.CloseGateway(ctx, activeRefs.References[0].GatewayID))
	require.Eventually(t, func() bool {
		refs, e := store.References(ctx, m.Scope, "A")
		return e == nil && len(refs.References) == 1
	}, time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		refs, e := store.References(ctx, m.Scope, "A")
		return e == nil && len(refs.References) == 2
	}, 21*time.Second, 20*time.Millisecond, "Gateway必须从仍活跃的原会话恢复引用，不要求客户端重新订阅")
	// 原处理进程仍健康：通过公共Store释放一个key；Core接管必须跟随租约而不是健康哈希。
	require.NoError(t, store.Release(ctx, original))
	// 恢复快照与真实退订交错；另一个Gateway的引用不能被关闭。
	g1.WriteWebSocket(t, c1, "unsub", "/api/owned-demo/view", nil)
	var next dto.Snapshot
	require.Eventually(t, func() bool {
		var ok bool
		next, ok = query(g2, "A")
		return ok && next.InstanceID != initial.InstanceID && next.Epoch > initial.Epoch && next.References == 1
	}, 6*time.Second, 20*time.Millisecond)
	require.NotEqual(t, initial.PID, next.PID, "两个独立处理进程都必须实际接管")
	refs, err := store.References(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Len(t, refs.References, 1)
	submit(g2, "A", 1)
	after := waitFrame(messages2, 2)
	require.Equal(t, next.InstanceID, after.InstanceID)
	require.Equal(t, next.Epoch, after.Epoch)
	require.Eventually(t, func() bool { s, ok := query(g1, "A"); return ok && s.Count == 2 && s.InstanceID == next.InstanceID }, 3*time.Second, 20*time.Millisecond, "原Gateway查询也必须跟随新owner")
	select {
	case frame := <-other:
		t.Fatalf("另一用户收到错误通知: event=%s channel=%s", frame.Event, frame.Channel)
	case <-time.After(200 * time.Millisecond):
	}
	for draining := true; draining; {
		select {
		case frame := <-messages1:
			require.Equal(t, "unsub", frame.Event, "退订后不能恢复旧引用或接收通知")
		default:
			draining = false
		}
	}
	t.Logf("真实处理PID=%d/%d，两个Gateway、三个认证WS客户端；SQL提交计数=2；无需重订，旧epoch=%d 新epoch=%d", initial.PID, next.PID, initial.Epoch, next.Epoch)
}
