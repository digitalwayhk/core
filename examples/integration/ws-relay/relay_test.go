// 本文件以真实双进程、mTLS与WS帧验证普通/归属路由共存，合成可信context不能代替本测试。
package wsrelay_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/mq"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

type relayState struct {
	PID   int                 `json:"pid"`
	Local map[string][]uint64 `json:"local"`
	Peer  map[string][]uint64 `json:"peer"`
	Nodes []*cluster.NodeInfo `json:"nodes"`
}

// TestWebSocketRelayCoexistenceRealMTLSGateways 验证真实双Gateway的普通/归属WS投递、会话生命周期与信任边界。
func TestWebSocketRelayCoexistenceRealMTLSGateways(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: 需要独占CORE_TEST_REDIS_ADDR")
	}
	ctx := context.Background()
	prefix := fmt.Sprintf("core:ws-relay:%d", time.Now().UnixNano())
	p := mq.NewRedisStreamProvider(addr, prefix, 0)
	require.NoError(t, p.Connect(ctx))
	defer p.Close()
	require.NoError(t, p.ProvisionOwnership(ctx, contract.Manifest(), 0))
	require.NoError(t, p.BindOwnership(ctx, contract.Manifest()))
	pki := integration.NewGRPCTestPKI(t, contract.ServiceName, "other-service")
	start := func(index int) *integration.Suite {
		id := pki.Services[contract.ServiceName]
		s, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/integration/ws-relay/testdata/gateway", BinaryName: "relay-gateway", TempPrefix: "core-relay-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: map[string]string{"CORE_TEST_REDIS_ADDR": addr, "CORE_RELAY_PREFIX": prefix, "CORE_RELAY_INDEX": fmt.Sprint(index), "CORE_RELAY_CA": pki.CAFile, "CORE_RELAY_CERT": id.CertFile, "CORE_RELAY_KEY": id.KeyFile}})
		require.NoError(t, err)
		t.Cleanup(func() {
			if t.Failed() {
				s.PrintLog()
			}
			s.Stop()
		})
		return s
	}
	g1, g2 := start(1), start(2)
	tokens := map[*integration.Suite]string{g1: g1.TokenFor(t, "A", 0), g2: g2.TokenFor(t, "A", 0)}
	state := func(s *integration.Suite) relayState {
		r := s.RequestJSON(t, http.MethodPost, "/api/owned-demo/control", tokens[s], map[string]interface{}{})
		require.True(t, r.Success, r.ErrorMessage)
		var out relayState
		require.NoError(t, json.Unmarshal(r.Data, &out))
		return out
	}
	require.Eventually(t, func() bool { return len(state(g1).Nodes) == 2 && len(state(g2).Nodes) == 2 }, 5*time.Second, 20*time.Millisecond)
	require.NotEqual(t, state(g1).PID, state(g2).PID, "不能用同进程两个对象充当横跨节点")
	tlsClient := func(identity string, certificate bool) pb.CoreTransportClient {
		ca, err := os.ReadFile(pki.CAFile)
		require.NoError(t, err)
		roots := x509.NewCertPool()
		require.True(t, roots.AppendCertsFromPEM(ca))
		cfg := &tls.Config{RootCAs: roots, ServerName: contract.ServiceName, MinVersion: tls.VersionTLS12}
		if certificate {
			id := pki.Services[identity]
			pair, err := tls.LoadX509KeyPair(id.CertFile, id.KeyFile)
			require.NoError(t, err)
			cfg.Certificates = []tls.Certificate{pair}
		}
		conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", gatewayGRPCPort(state(g2), g2.BasePort+1)), grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		return pb.NewCoreTransportClient(conn)
	}
	same, other, anonymous := tlsClient(contract.ServiceName, true), tlsClient("other-service", true), tlsClient("", false)
	call := func(client pb.CoreTransportClient, source, path string, data interface{}) error {
		raw, err := json.Marshal(data)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		res, err := client.Call(ctx, &pb.PayloadRequest{SourceService: source, TargetService: contract.ServiceName, TargetPath: path, HttpMethod: http.MethodPost, Data: raw, Auth: true})
		if err != nil {
			return err
		}
		var envelope integration.ResponseEnvelope
		if err = json.Unmarshal(res.Data, &envelope); err != nil {
			return err
		}
		if !envelope.Success {
			return fmt.Errorf("中继返回失败: %d", envelope.ErrorCode)
		}
		return nil
	}
	connect := func(s *integration.Suite, uid, path string) *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial(s.WebSocketURL, nil)
		require.NoError(t, err)
		t.Cleanup(func() { c.Close() })
		if uid != "public" {
			token := s.TokenFor(t, uid, 0)
			s.WriteWebSocket(t, c, "sub", "logon", map[string]string{"token": token})
			require.Equal(t, "success", s.ReadWebSocket(t, c, 3*time.Second).Event)
		}
		s.WriteWebSocket(t, c, "sub", path, map[string]string{"userID": "forged"})
		reply := s.ReadWebSocket(t, c, 3*time.Second)
		require.Equal(t, "sub", reply.Event, string(reply.Data))
		return c
	}
	frame := func(s *integration.Suite, c *websocket.Conn, path, uid string, sequence int) {
		value := s.ReadWebSocket(t, c, 4*time.Second)
		require.Equal(t, path, value.Channel)
		var data struct {
			UserID   string
			Sequence int
		}
		require.NoError(t, json.Unmarshal(value.Data, &data))
		require.Equal(t, uid, data.UserID)
		require.Equal(t, sequence, data.Sequence)
	}
	const sub = "/api/servermanage/ws/subscription"
	const notice = "/api/servermanage/ws/notice"
	for _, path := range []string{"/api/owned-demo/ordinary-private", "/api/owned-demo/ordinary-public", "/api/owned-demo/owned"} {
		t.Run(path, func(t *testing.T) {
			uid := "A"
			if path == "/api/owned-demo/ordinary-public" {
				uid = "public"
			}
			hash := utils.HashCode64(uid)
			c1, c2 := connect(g1, uid, path), connect(g2, uid, path)
			t.Cleanup(func() { c1.Close(); c2.Close() })
			// 两条正式中继分别取证：先实际远端索引，再真实mTLS notice帧。
			indexed := assert.Eventually(t, func() bool { return contains(state(g1).Peer[path], hash) && contains(state(g2).Peer[path], hash) }, 4*time.Second, 20*time.Millisecond, "普通WS摘要必须进入对等索引")
			err := call(same, contract.ServiceName, notice, map[string]interface{}{"route_path": path, "hash": hash, "message": map[string]interface{}{"userID": uid, "sequence": 1}})
			assert.NoError(t, err, "普通WS通知必须到达原正式处理器")
			if err == nil {
				frame(g2, c2, path, uid, 1)
			}
			if !indexed || err != nil {
				return
			}
			// 从没有替代转发逻辑的正式NoticeWebSocket发布；对端必须收到实际业务帧。
			publish := func(s *integration.Suite, id string, seq int) {
				r := s.RequestJSON(t, http.MethodPost, "/api/owned-demo/control", tokens[s], map[string]interface{}{"route": path, "userID": id, "sequence": seq})
				require.True(t, r.Success, r.ErrorMessage)
			}
			publish(g1, uid, 2)
			frame(g1, c1, path, uid, 2)
			frame(g2, c2, path, uid, 2)
			if uid == "A" {
				b := connect(g2, "B", path)
				require.Eventually(t, func() bool { return contains(state(g1).Peer[path], utils.HashCode64("B")) }, 4*time.Second, 20*time.Millisecond)
				publish(g1, "A", 3)
				frame(g1, c1, path, "A", 3)
				frame(g2, c2, path, "A", 3)
				publish(g1, "B", 4)
				frame(g2, b, path, "B", 4) // 若误投A，首帧断言立即失败。
				b.Close()
				require.Eventually(t, func() bool { return !contains(state(g1).Peer[path], utils.HashCode64("B")) }, 4*time.Second, 20*time.Millisecond, "关闭连接须移除对等索引")
			}
			publish(g2, uid, 6)
			frame(g1, c1, path, uid, 6)
			frame(g2, c2, path, uid, 6)
			g2.WriteWebSocket(t, c2, "unsub", path, nil)
			require.Equal(t, "unsub", g2.ReadWebSocket(t, c2, 3*time.Second).Event)
			require.Eventually(t, func() bool { return !contains(state(g1).Peer[path], hash) }, 4*time.Second, 20*time.Millisecond, "退订须真正移除对等索引")
			publish(g1, uid, 5)
			frame(g1, c1, path, uid, 5)
			c2.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			_, _, err = c2.ReadMessage()
			require.Error(t, err, "退订后不得投递")
			var timeout net.Error
			require.ErrorAs(t, err, &timeout, "退订应保持连接，不可通过关闭连接伪造无消息")
			require.True(t, timeout.Timeout())
			c1.Close()
			require.Eventually(t, func() bool { return !contains(state(g2).Peer[path], hash) }, 4*time.Second, 20*time.Millisecond)
		})
	}
	t.Run("拒绝边界", func(t *testing.T) {
		path := "/api/owned-demo/ordinary-private"
		data := map[string]interface{}{"route_path": path, "hash": utils.HashCode64("A"), "node_id": "unknown-node", "active": true}
		for _, relay := range []string{sub, notice} {
			body := map[string]interface{}{"route_path": path, "hash": utils.HashCode64("A"), "node_id": "unknown-node", "active": true, "message": map[string]string{"userID": "A"}}
			r := g2.RequestJSON(t, http.MethodPost, relay, "", body)
			assert.False(t, r.Success, "匿名HTTP不得进入中继")
			assert.Equal(t, http.StatusForbidden, r.HTTPStatus)
			// 带合法用户token、自报SourceService也不能建立内部服务身份。
			r = g2.RequestJSON(t, http.MethodPost, relay, tokens[g2], body)
			assert.False(t, r.Success, "用户token不能替代mTLS服务身份")
			assert.Equal(t, http.StatusForbidden, r.HTTPStatus)
			assert.Error(t, call(anonymous, contract.ServiceName, relay, body))
			assert.Equal(t, codes.Unauthenticated, status.Code(call(same, "other-service", relay, body)), "SAN与自报SourceService不一致")
			assert.Equal(t, codes.Internal, status.Code(call(other, "other-service", relay, body)), "异服务证书不得进入同服务中继")
			body["route_path"] = "/api/owned-demo/unknown"
			assert.Error(t, call(same, contract.ServiceName, relay, body))
		}
		assert.Error(t, call(same, contract.ServiceName, sub, data), "未知节点不得写对等索引")
		discovery, err := cluster.NewRedisProvider(addr, 0, prefix+":discovery", time.Minute)
		require.NoError(t, err)
		defer discovery.Close()
		require.NoError(t, discovery.Register(ctx, &cluster.NodeInfo{ID: "foreign-node", ServiceName: "other-service", ServiceInstanceID: "foreign-instance", Address: "127.0.0.1", Port: 19001, DataCenterID: 9, MachineID: 9}))
		data["node_id"] = "foreign-node"
		assert.Error(t, call(same, contract.ServiceName, sub, data), "已注册异服务节点也不得写索引")
		require.NoError(t, discovery.Deregister(ctx, "foreign-node"))
		assert.Error(t, call(same, contract.ServiceName, "/api/servermanage/other", data), "两个特例不授予其它管理路径权限")
		assert.Error(t, call(same, contract.ServiceName, "/api/servermanage/getmenu", map[string]interface{}{}), "已注册的ServerManage也不能因同服务mTLS获得管理权限")
		// 私有订阅不得接受匿名或伪造JSON用户身份。
		c, _, err := websocket.DefaultDialer.Dial(g2.WebSocketURL, nil)
		require.NoError(t, err)
		defer c.Close()
		g2.WriteWebSocket(t, c, "sub", path, map[string]string{"userID": "A"})
		require.Equal(t, "error", g2.ReadWebSocket(t, c, 3*time.Second).Event)
	})
	t.Logf("Gateway PID=%d/%d；双向摘要与真实WS帧、用户隔离、退订/关闭、真实mTLS拒绝边界", state(g1).PID, state(g2).PID)
}
func contains(values []uint64, want uint64) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func gatewayGRPCPort(state relayState, httpPort int) int {
	for _, node := range state.Nodes {
		if node.Port == httpPort {
			return node.GRPCPort
		}
	}
	return 0
}
