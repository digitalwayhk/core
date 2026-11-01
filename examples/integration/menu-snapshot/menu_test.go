// 本文件通过真实双进程、mTLS、Redis发现及SQLite控制面验证菜单恢复和身份边界。
package menusnapshot_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestMenuDiscoveryRealMTLSPeers 验证正式菜单命令经过对端入站处理器，落库后重启仍可读取。
func TestMenuDiscoveryRealMTLSPeers(t *testing.T) {
	redisAddr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("NOT_RUN: 需要独占 CORE_TEST_REDIS_ADDR")
	}
	pki := integration.NewGRPCTestPKI(t, "menu-peer", "other-service")
	prefix := fmt.Sprintf("core:menu-snapshot:%d", time.Now().UnixNano())
	start := func(index int) (*integration.Suite, *integration.Suite) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		require.NoError(t, err)
		viewPort := listener.Addr().(*net.TCPAddr).Port
		require.NoError(t, listener.Close())
		id := pki.Services["menu-peer"]
		s, err := integration.StartProcess(integration.ProcessOptions{
			BuildPackage: "./examples/integration/menu-snapshot/testdata/service", BinaryName: "menu-peer",
			TempPrefix: "core-menu-peer-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2,
			Arguments: []string{"-view", fmt.Sprint(viewPort)}, Environment: map[string]string{
				"CORE_TEST_REDIS_ADDR": redisAddr, "CORE_MENU_INDEX": fmt.Sprint(index), "CORE_MENU_PREFIX": prefix,
				"CORE_MENU_CA": pki.CAFile, "CORE_MENU_CERT": id.CertFile, "CORE_MENU_KEY": id.KeyFile}})
		require.NoError(t, err)
		t.Cleanup(func() {
			if t.Failed() {
				s.PrintLog()
			}
			s.Stop()
		})
		view := &integration.Suite{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", viewPort)}
		require.Eventually(t, func() bool {
			c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", viewPort), 100*time.Millisecond)
			if e != nil {
				return false
			}
			c.Close()
			return true
		}, 10*time.Second, 25*time.Millisecond)
		return s, view
	}
	a, viewA := start(1)
	b, _ := start(2)
	admin := viewA.TokenFor(t, "menu-admin", 1)
	// 即便带管理令牌，HTTP也没有注册内部菜单协议。
	for _, s := range []*integration.Suite{a, b} {
		r := s.RequestJSON(t, http.MethodPost, "/internal/core/menu-snapshot", s.TokenFor(t, "admin", 1), nil)
		require.Equal(t, http.StatusNotFound, r.HTTPStatus, r.Body)
	}
	call := func(identity, source, path string, withCert bool) (*pb.PayloadResponse, error) {
		ca, err := os.ReadFile(pki.CAFile)
		require.NoError(t, err)
		roots := x509.NewCertPool()
		require.True(t, roots.AppendCertsFromPEM(ca))
		cfg := &tls.Config{RootCAs: roots, ServerName: "menu-peer", MinVersion: tls.VersionTLS12}
		if withCert {
			id := pki.Services[identity]
			pair, e := tls.LoadX509KeyPair(id.CertFile, id.KeyFile)
			require.NoError(t, e)
			cfg.Certificates = []tls.Certificate{pair}
		}
		// gRPC覆盖端口按服务名字典序分配，menu-peer在server之前。
		conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", b.GRPCBasePort), grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
		require.NoError(t, err)
		defer conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return pb.NewCoreTransportClient(conn).Call(ctx, &pb.PayloadRequest{SourceService: source, TargetService: "menu-peer", TargetPath: path, HttpMethod: http.MethodPost, Data: []byte(`{}`)})
	}
	response, err := call("other-service", "other-service", "/internal/core/menu-snapshot", true)
	require.NoError(t, err)
	var envelope struct {
		Success bool                        `json:"success"`
		Data    smodels.MenuServiceSnapshot `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Data, &envelope))
	require.True(t, envelope.Success)
	require.Equal(t, "menu-peer", envelope.Data.Name)
	require.Contains(t, string(response.Data), "/api/manage/menu-peer/itemmanage/search")
	_, err = call("other-service", "menu-peer", "/internal/core/menu-snapshot", true)
	require.Error(t, err, "SAN与Source不一致必须拒绝")
	_, err = call("", "menu-peer", "/internal/core/menu-snapshot", false)
	require.Error(t, err, "缺客户端证书必须拒绝")
	_, err = call("menu-peer", "menu-peer", "/api/servermanage/queryrouters", true)
	require.Error(t, err, "同服务证书不授予ServerManage权限")
	// 等待对端完成发现，正式更新会逐副本核对快照；错误不以局部菜单替代成功。
	time.Sleep(2 * time.Second)
	beforeA, beforeB := a.TransportStats(t), b.TransportStats(t)
	emptySearch := viewA.RequestJSON(t, http.MethodPost, "/api/manage/server/menumanage/search", admin, map[string]int{"page": 1, "size": 100})
	require.True(t, emptySearch.Success, emptySearch.Body)
	require.Contains(t, string(emptySearch.Data), "跨进程菜单回归")
	update := viewA.RequestJSON(t, http.MethodPost, "/api/manage/server/menumanage/updatemenu", admin, nil)
	require.True(t, update.Success, update.Body)
	afterA, afterB := a.TransportStats(t), b.TransportStats(t)
	require.Greater(t, afterA.Transport.GRPCSelected, beforeA.Transport.GRPCSelected, "更新必须真实调用对端，不能退化为单进程菜单")
	require.Greater(t, afterB.Transport.InboundGRPC, beforeB.Transport.InboundGRPC)
	require.Equal(t, uint64(0), afterA.Transport.HTTPSelected)
	require.Equal(t, uint64(0), afterA.Transport.HTTPFallback)
	search := viewA.RequestJSON(t, http.MethodPost, "/api/manage/server/menumanage/search", admin, map[string]int{"page": 1, "size": 100})
	require.True(t, search.Success, search.Body)
	require.Contains(t, string(search.Data), "跨进程菜单回归")
	menus := viewA.RequestJSON(t, http.MethodGet, "/api/servermanage/getmenu", "", nil)
	require.True(t, menus.Success, menus.Body)
	require.Contains(t, string(menus.Data), "/api/manage/menu-peer/itemmanage/search")
	require.NoError(t, a.Restart())
	recovered := viewA.RequestJSON(t, http.MethodGet, "/api/servermanage/getmenu", "", nil)
	require.True(t, recovered.Success, recovered.Body)
	require.Contains(t, string(recovered.Data), "跨进程菜单回归")
}
