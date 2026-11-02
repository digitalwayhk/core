// 本文件用两个真实处理进程、两个真实Gateway、MySQL与WS客户端验证非owner查询及免重订恢复。
package scopedownership_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/dto"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestScopedOwnershipRealGatewaysSQLAndWebSockets(t *testing.T) {
	testScopedGateways(t, false, false)
}
func TestScopedDynamicOwnershipRealGatewaysSQLAndWebSockets(t *testing.T) {
	testScopedGateways(t, true, false)
}
func TestScopedFixedToDynamicMigrationRealSQLAndGateways(t *testing.T) {
	testScopedGateways(t, true, true)
}
func testScopedGateways(t *testing.T, dynamic, migrate bool) {
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
	if dynamic {
		m = contract.DynamicManifest()
	}
	dynamicNow := dynamic && !migrate
	single := migrate
	if migrate {
		m = contract.Manifest()
		m.Keys = []string{"A"}
		m.MaxMessageBytes = 64 << 10
	}
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	require.NoError(t, p.BindOwnership(ctx, m))
	if dynamicNow {
		require.NoError(t, p.RegisterOwnershipKey(ctx, m, "A"))
	}
	store, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	start := func(role string, index, capacity int) *integration.Suite {
		mode := "0"
		if dynamicNow {
			mode = "1"
		}
		fixedSingle := "0"
		if single {
			fixedSingle = "1"
		}
		s, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/11-scoped-event-ownership/main", BinaryName: fmt.Sprintf("demo11-%s-%d", role, index), TempPrefix: "core-demo11-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: map[string]string{"CORE_DEMO11_DYNAMIC_KEYS": mode, "CORE_DEMO11_FIXED_SINGLE_KEY": fixedSingle, "CORE_DEMO11_CA_FILE": pki.CAFile, "CORE_DEMO11_CERT_FILE": identity.CertFile, "CORE_DEMO11_KEY_FILE": identity.KeyFile, "CORE_DEMO11_ROLE": role, "CORE_DEMO11_INDEX": strconv.Itoa(index), "CORE_DEMO11_REDIS_ADDR": addr, "CORE_DEMO11_PREFIX": prefix, "CORE_DEMO11_MYSQL_CONFIG_FILE": file, "CORE_DEMO11_DATABASE": database, "CORE_DEMO11_MAX_OWNED": strconv.Itoa(capacity)}})
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
	baselineCount := int64(0)
	if migrate {
		floor := original.Epoch
		a.StopProcess()
		require.Eventually(t, func() bool { _, e := store.Resolve(ctx, m.Scope, "A"); return e != nil }, 3*time.Second, 10*time.Millisecond)
		env := event.NewEnvelope(contract.ServiceName, "input", []byte(`{"key":"A"}`))
		env.Subject = m.Subjects[0]
		env.ShardKey = "user:A"
		raw, e := json.Marshal(env)
		require.NoError(t, e)
		require.NoError(t, p.Publish(ctx, env.Subject, raw, &mq.PublishOptions{OrderingKey: env.ShardKey}))
		admin := redis.NewClient(&redis.Options{Addr: addr})
		defer admin.Close()
		before, e := admin.XReadGroup(ctx, &redis.XReadGroupArgs{Group: m.Group, Consumer: "old-preserved", Streams: []string{prefix + ":" + env.Subject, ">"}, Count: 1, Block: -1}).Result()
		require.NoError(t, e)
		id := before[0].Messages[0].ID
		target := contract.DynamicManifest()
		require.NoError(t, p.MigrateOwnershipKeys(ctx, m, target, floor))
		m = target
		dynamicNow = true
		single = false
		after, e := admin.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: prefix + ":" + env.Subject, Group: m.Group, Start: "-", End: "+", Count: 1}).Result()
		require.NoError(t, e)
		require.Len(t, after, 1)
		require.Equal(t, id, after[0].ID)
		a = start("worker", 1, 1)
		require.Eventually(t, func() bool {
			var e error
			original, e = store.Resolve(ctx, m.Scope, "A")
			return e == nil && original.Epoch > floor
		}, 3*time.Second, 10*time.Millisecond)
		baselineCount = 1
	}
	var originalBID string
	admin := redis.NewClient(&redis.Options{Addr: addr})
	defer admin.Close()
	if dynamic {
		env := event.NewEnvelope(contract.ServiceName, "input", []byte(`{"key":"B"}`))
		env.Subject = m.Subjects[0]
		env.ShardKey = "user:B"
		raw, e := json.Marshal(env)
		require.NoError(t, e)
		require.NoError(t, p.Publish(ctx, m.Subjects[0], raw, &mq.PublishOptions{OrderingKey: env.ShardKey}))
		require.Eventually(t, func() bool { return admin.XPending(ctx, prefix+":"+m.Subjects[0], m.Group).Val().Count == 1 }, 3*time.Second, 10*time.Millisecond, "新key先于注册的原消息进入PEL，不允许提前ACK")
		pending, e := admin.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: prefix + ":" + m.Subjects[0], Group: m.Group, Start: "-", End: "+", Count: 1}).Result()
		require.NoError(t, e)
		originalBID = pending[0].ID
		_, e = store.Resolve(ctx, m.Scope, "B")
		require.Error(t, e, "未注册不能Ready")
		require.NoError(t, p.RegisterOwnershipKey(ctx, m, "B"))
	}
	capacity := 2
	if dynamic {
		capacity = 3
	} // A释放后需有实际接管槽位；B/C已占两个槽位，不能凭延长等待伪造容量。
	b := start("worker", 2, capacity)
	if dynamic {
		require.Eventually(t, func() bool { _, e := store.Resolve(ctx, m.Scope, "B"); return e == nil }, 3*time.Second, 10*time.Millisecond)
		require.NoError(t, p.RegisterOwnershipKey(ctx, m, "C"))
		require.Eventually(t, func() bool { _, e := store.Resolve(ctx, m.Scope, "C"); return e == nil }, 3*time.Second, 10*time.Millisecond, "两个进程启动后追加C，不重启整组")
		ownerB, e := store.Resolve(ctx, m.Scope, "B")
		require.NoError(t, e)
		ownerC, e := store.Resolve(ctx, m.Scope, "C")
		require.NoError(t, e)
		require.Equal(t, ownerB.InstanceID, ownerC.InstanceID, "单实例应可持多个key")
	}
	g1, g2 := start("gateway", 3, 1), start("gateway", 4, 1)
	tokens := map[*integration.Suite]map[string]string{}
	for _, s := range []*integration.Suite{a, b, g1, g2} {
		tokens[s] = map[string]string{"A": s.TokenFor(t, "A", 0), "B": s.TokenFor(t, "B", 0), "C": s.TokenFor(t, "C", 0)}
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
	if dynamic {
		require.Eventually(t, func() bool { v, ok := query(g1, "B"); return ok && v.Count == 1 }, 5*time.Second, 20*time.Millisecond, "注册后消费同一原消息并提交SQL")
		require.Zero(t, admin.XPending(ctx, prefix+":"+m.Subjects[0], m.Group).Val().Count)
		raw, e := admin.XRangeN(ctx, prefix+":"+m.Subjects[0], originalBID, originalBID, 1).Result()
		require.NoError(t, e)
		require.Len(t, raw, 1, "原消息历史不得删除")
		require.Eventually(t, func() bool { _, ok := query(g2, "C"); return ok }, 3*time.Second, 20*time.Millisecond, "新key在双Gateway上走真实mTLS Ready查询")
	}
	if migrate {
		require.Eventually(t, func() bool { s, ok := query(g1, "A"); return ok && s.Count == baselineCount }, 3*time.Second, 20*time.Millisecond, "固定→动态后原PEL消息在真实SQL中收敛")
	}
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
	one := waitFrame(messages1, baselineCount+1)
	two := waitFrame(messages2, baselineCount+1)
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
	handoverStarted := time.Now()
	require.NoError(t, store.Release(ctx, original))
	// 恢复快照与真实退订交错；另一个Gateway的引用不能被关闭。
	g1.WriteWebSocket(t, c1, "unsub", "/api/owned-demo/view", nil)
	var next dto.Snapshot
	require.Eventually(t, func() bool {
		var ok bool
		next, ok = query(g2, "A")
		return ok && next.InstanceID != initial.InstanceID && next.Epoch > initial.Epoch && next.References == 1
	}, 6*time.Second, 20*time.Millisecond)
	t.Logf("RECOVERY graceful_release_to_ready_ms=%.3f", float64(time.Since(handoverStarted))/float64(time.Millisecond))
	require.NotEqual(t, initial.PID, next.PID, "两个独立处理进程都必须实际接管")
	refs, err := store.References(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Len(t, refs.References, 1)
	submit(g2, "A", 1)
	after := waitFrame(messages2, baselineCount+2)
	require.Equal(t, next.InstanceID, after.InstanceID)
	require.Equal(t, next.Epoch, after.Epoch)
	require.Eventually(t, func() bool {
		s, ok := query(g1, "A")
		return ok && s.Count == baselineCount+2 && s.InstanceID == next.InstanceID
	}, 3*time.Second, 20*time.Millisecond, "原Gateway查询也必须跟随新owner")
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
	if dynamic {
		hardExitStarted := time.Now()
		b.KillProcess()
		replacement := start("worker", 5, 3)
		_ = replacement
		require.Eventually(t, func() bool {
			state, ok := query(g2, "A")
			return ok && state.Count == baselineCount+2 && state.Epoch > next.Epoch && state.PID != next.PID && state.References == 1 && state.RelayReady
		}, 20*time.Second, 50*time.Millisecond, "硬崩溃后原Gateway和SQL fence须恢复")
		t.Logf("RECOVERY hard_exit_to_ready_and_relay_ms=%.3f (包含替代进程构建/启动)", float64(time.Since(hardExitStarted))/float64(time.Millisecond))
		submit(g2, "A", 0)
		recovered := waitFrame(messages2, baselineCount+3)
		require.Greater(t, recovered.Epoch, next.Epoch)
	}
	t.Logf("真实处理PID=%d/%d，两个Gateway、三个认证WS客户端；SQL提交计数=2；无需重订，旧epoch=%d 新epoch=%d", initial.PID, next.PID, initial.Epoch, next.Epoch)
}
