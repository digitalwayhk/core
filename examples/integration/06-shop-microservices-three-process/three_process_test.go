// 本文件验证 06 示例在用户、供应商、订单三个独立进程下的服务发现、mTLS gRPC 和内部调用链。
// 测试重点是跨进程不走 HTTP fallback，内部调用方身份由证书 SAN 与逻辑服务名共同确认。
package shopmicroservices_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// TestThreeProcessDiscoveryAndRemoteCalls 验证三进程启动、Redis 发现、mTLS 健康检查和 User -> Order -> Supplier gRPC 调用链。
func TestThreeProcessDiscoveryAndRemoteCalls(t *testing.T) {
	pki := integration.NewGRPCTestPKI(t, "shop-user", "shop-supplier", "shop-order")
	redisPrefix := "core:test:06:three-process:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	assertPKIFileModes(t, pki)
	assertPKISANs(t, pki)
	parentCA, parentCASet := os.LookupEnv("SHOP_GRPC_CA_FILE")
	user, supplier, order := startShopProcesses(t, pki, redisPrefix)
	defer user.Stop()
	assertParentEnvironmentUnchanged(t, "SHOP_GRPC_CA_FILE", parentCA, parentCASet)
	defer supplier.Stop()
	defer order.Stop()
	processes := []*integration.Suite{user, supplier, order}
	waitProcessReady(t, user, "/api/health", processes...)
	waitProcessReady(t, supplier, "/api/health", processes...)
	waitProcessReady(t, order, "/api/health", processes...)
	grpcPorts := []int{
		readServiceGRPCPort(t, user, "shop-user"),
		readServiceGRPCPort(t, supplier, "shop-supplier"),
		readServiceGRPCPort(t, order, "shop-order"),
	}
	for _, port := range grpcPorts {
		require.Positive(t, port)
		require.NotEqual(t, 18080, port)
	}
	for index, serviceName := range []string{"shop-user", "shop-supplier", "shop-order"} {
		assertGRPCHealthServing(t, grpcPorts[index], serviceName, pki)
	}
	assertGRPCHealthFails(t, grpcPorts[0], "shop-order", pki)
	waitProcessReady(t, user, "/api/shop-user/getproducts", processes...)
	before := make([]integration.TransportStatsSnapshot, len(processes))
	for index, process := range processes {
		before[index] = process.TransportStats(t)
		require.Empty(t, before[index].Fallback)
		require.Zero(t, before[index].Transport.HTTPSelected)
	}

	supplierToken := supplier.TokenFor(t, "supplier-remote", 1)
	createdProduct := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/add", supplierToken, map[string]interface{}{"name": "远程商品", "code": "remote-product", "price": "9.90"})
	require.True(t, createdProduct.Success, createdProduct.ErrorMessage)
	var productRaw struct {
		ID         string `json:"id"`
		SupplierID uint   `json:"supplierID"`
		Name       string `json:"name"`
		Code       string `json:"code"`
		Price      string `json:"price"`
	}
	require.NoError(t, json.Unmarshal(createdProduct.Data, &productRaw))
	productID, err := strconv.ParseUint(productRaw.ID, 10, 64)
	require.NoError(t, err)
	product := supplierdto.Product{ID: uint(productID), SupplierID: productRaw.SupplierID, Name: productRaw.Name, Code: productRaw.Code, Price: decimal.RequireFromString(productRaw.Price)}
	updated := supplier.RequestJSON(t, http.MethodPost, "/api/manage/shop-supplier/productmanage/setproductenabled", supplierToken, map[string]interface{}{"id": productRaw.ID, "enabled": true})
	require.True(t, updated.Success, updated.ErrorMessage)

	userManageToken := user.TokenFor(t, "buyer-remote", 1)
	userToken := user.TokenFor(t, "buyer-remote", 0)
	createdAddress := user.RequestJSON(t, http.MethodPost, "/api/manage/shop-user/addressmanage/add", userManageToken, map[string]interface{}{"recipient": "远程用户", "detail": "2 号"})
	require.True(t, createdAddress.Success, createdAddress.ErrorMessage)
	var addressRaw struct {
		ID        string `json:"id"`
		Recipient string `json:"recipient"`
		Detail    string `json:"detail"`
	}
	require.NoError(t, json.Unmarshal(createdAddress.Data, &addressRaw))
	addressID, err := strconv.ParseUint(addressRaw.ID, 10, 64)
	require.NoError(t, err)
	createdOrder := user.RequestJSON(t, http.MethodPost, "/api/shop-user/addorder", userToken, map[string]interface{}{"requestID": "remote-request-1", "productID": product.ID, "quantity": 3, "addressID": addressID})
	if !createdOrder.Success {
		for index, process := range processes {
			t.Logf("process %d transport stats: %+v", index, process.TransportStats(t))
		}
		user.PrintLog()
		supplier.PrintLog()
		order.PrintLog()
	}
	require.True(t, createdOrder.Success, createdOrder.ErrorMessage)
	var result orderdto.Order
	require.NoError(t, json.Unmarshal(createdOrder.Data, &result))
	assert.Equal(t, product.SupplierID, result.Product.SupplierID)
	assert.Equal(t, 3, result.Quantity)

	after := make([]integration.TransportStatsSnapshot, len(processes))
	var grpcDelta, httpDelta uint64
	for index, process := range processes {
		after[index] = process.TransportStats(t)
		require.Empty(t, after[index].Fallback)
		require.GreaterOrEqual(t, after[index].Transport.GRPCSelected, before[index].Transport.GRPCSelected)
		require.GreaterOrEqual(t, after[index].Transport.HTTPSelected, before[index].Transport.HTTPSelected)
		grpcDelta += after[index].Transport.GRPCSelected - before[index].Transport.GRPCSelected
		httpDelta += after[index].Transport.HTTPSelected - before[index].Transport.HTTPSelected
		require.Zero(t, after[index].Transport.HTTPSelected)
		require.Zero(t, before[index].Transport.SendFailure)
		require.Zero(t, after[index].Transport.SendFailure)
	}
	require.Positive(t, grpcDelta)
	require.Zero(t, httpDelta)
	require.Positive(t, after[0].Transport.GRPCSelected-before[0].Transport.GRPCSelected, "User -> Order 必须选中 gRPC")
	require.Positive(t, after[2].Transport.InboundGRPC-before[2].Transport.InboundGRPC, "Order 必须收到 User 的 gRPC")
	require.Positive(t, after[2].Transport.GRPCSelected-before[2].Transport.GRPCSelected, "Order -> Supplier 必须选中 gRPC")
	require.Positive(t, after[1].Transport.InboundGRPC-before[1].Transport.InboundGRPC, "Supplier 必须收到 Order 的 gRPC")

	wrongPKI := integration.NewGRPCTestPKI(t, "wrong-client")
	assertGRPCHealthFails(t, grpcPorts[0], "shop-user", wrongPKI)
}

// startShopProcesses 并发启动用户、供应商和订单三个真实进程，使用同一 Redis 前缀和独立 mTLS 身份。
func startShopProcesses(t *testing.T, pki *integration.GRPCTestPKI, redisPrefix string) (*integration.Suite, *integration.Suite, *integration.Suite) {
	t.Helper()
	specs := []integration.ProcessOptions{
		{BuildPackage: "./examples/06-shop-microservices/main/user", BinaryName: "shop-user", TempPrefix: "core-shop-user-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: processEnvironment(pki, "shop-user", redisPrefix), DisableRace: !integration.IsRaceRun()},
		{BuildPackage: "./examples/06-shop-microservices/main/supplier", BinaryName: "shop-supplier", TempPrefix: "core-shop-supplier-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: processEnvironment(pki, "shop-supplier", redisPrefix), DisableRace: !integration.IsRaceRun()},
		{BuildPackage: "./examples/06-shop-microservices/main/order", BinaryName: "shop-order", TempPrefix: "core-shop-order-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: processEnvironment(pki, "shop-order", redisPrefix), DisableRace: !integration.IsRaceRun()},
	}
	results := make([]*integration.Suite, len(specs))
	errs := make([]error, len(specs))
	var wg sync.WaitGroup
	for index := range specs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = integration.StartProcess(specs[index])
		}(index)
	}
	wg.Wait()
	for index, err := range errs {
		if err != nil {
			for _, suite := range results {
				if suite != nil {
					suite.Stop()
				}
			}
			require.NoErrorf(t, err, "启动进程 %s", specs[index].BinaryName)
		}
	}
	return results[0], results[1], results[2]
}

// readServiceGRPCPort 从进程落盘配置中读取实际 gRPC 端口，验证内部调用不依赖固定端口。
func readServiceGRPCPort(t *testing.T, suite *integration.Suite, serviceName string) int {
	t.Helper()
	path := filepath.Join(suite.RootDir, "etc", serviceName+".json")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		contents, err := os.ReadFile(path)
		if err == nil {
			var stored struct {
				Transport struct {
					GRPC struct {
						Port int
					}
				}
			}
			require.NoError(t, json.Unmarshal(contents, &stored))
			return stored.Transport.GRPC.Port
		}
		time.Sleep(50 * time.Millisecond)
	}
	suite.PrintLog()
	t.Fatalf("gRPC config was not persisted for %s", serviceName)
	return 0
}

// processEnvironment 为单个服务进程构造 mTLS 和 Redis 发现/事件隔离环境变量。
func processEnvironment(pki *integration.GRPCTestPKI, serviceName, redisPrefix string) map[string]string {
	identity := pki.Services[serviceName]
	return map[string]string{
		"SHOP_GRPC_CA_FILE":           pki.CAFile,
		"SHOP_GRPC_CERT_FILE":         identity.CertFile,
		"SHOP_GRPC_KEY_FILE":          identity.KeyFile,
		"SHOP_GRPC_SERVER_NAME":       "{service}",
		"SHOP_REDIS_DISCOVERY_PREFIX": redisPrefix + ":discovery",
		"SHOP_REDIS_EVENT_PREFIX":     redisPrefix + ":event",
	}
}

// assertParentEnvironmentUnchanged 验证测试进程启动不会污染父进程环境变量。
func assertParentEnvironmentUnchanged(t *testing.T, key, before string, existed bool) {
	t.Helper()
	after, stillExists := os.LookupEnv(key)
	require.Equal(t, existed, stillExists)
	require.Equal(t, before, after)
}

// assertPKIFileModes 验证测试生成的证书和私钥文件权限符合最小暴露要求。
func assertPKIFileModes(t *testing.T, pki *integration.GRPCTestPKI) {
	t.Helper()
	files := []string{pki.CAFile, pki.Client.CertFile, pki.Client.KeyFile}
	for _, identity := range pki.Services {
		files = append(files, identity.CertFile, identity.KeyFile)
	}
	for _, path := range files {
		info, err := os.Stat(path)
		require.NoError(t, err)
		want := os.FileMode(0o644)
		if len(path) >= 4 && path[len(path)-4:] == ".key" {
			want = 0o600
		}
		require.Equal(t, want, info.Mode().Perm(), path)
	}
}

// assertPKISANs 验证每个服务证书包含逻辑服务名、localhost 和本地回环地址。
func assertPKISANs(t *testing.T, pki *integration.GRPCTestPKI) {
	t.Helper()
	for serviceName, identity := range pki.Services {
		contents, err := os.ReadFile(identity.CertFile)
		require.NoError(t, err)
		block, _ := pem.Decode(contents)
		require.NotNil(t, block)
		certificate, err := x509.ParseCertificate(block.Bytes)
		require.NoError(t, err)
		require.Contains(t, certificate.DNSNames, serviceName)
		require.Contains(t, certificate.DNSNames, "localhost")
		require.NoError(t, certificate.VerifyHostname("127.0.0.1"))
	}
}

// clientTLSConfig 构造测试客户端 mTLS 配置，用于验证服务名匹配和错误 SAN 拒绝。
func clientTLSConfig(t *testing.T, pki *integration.GRPCTestPKI, serverName string) *tls.Config {
	t.Helper()
	caPEM, err := os.ReadFile(pki.CAFile)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM))
	certificate, err := tls.LoadX509KeyPair(pki.Client.CertFile, pki.Client.KeyFile)
	require.NoError(t, err)
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: serverName}
}

// assertGRPCHealthServing 验证给定服务名的 mTLS gRPC 健康检查可以成功。
func assertGRPCHealthServing(t *testing.T, port int, serverName string, pki *integration.GRPCTestPKI) {
	t.Helper()
	connection, err := googlegrpc.NewClient(fmt.Sprintf("127.0.0.1:%d", port), googlegrpc.WithTransportCredentials(credentials.NewTLS(clientTLSConfig(t, pki, serverName))))
	require.NoError(t, err)
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := grpc_health_v1.NewHealthClient(connection).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.NoError(t, err)
	require.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, response.Status)
}

// assertGRPCHealthFails 验证错误服务名或错误证书不能通过 mTLS gRPC 健康检查。
func assertGRPCHealthFails(t *testing.T, port int, serverName string, pki *integration.GRPCTestPKI) {
	t.Helper()
	connection, err := googlegrpc.NewClient(fmt.Sprintf("127.0.0.1:%d", port), googlegrpc.WithTransportCredentials(credentials.NewTLS(clientTLSConfig(t, pki, serverName))))
	require.NoError(t, err)
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = grpc_health_v1.NewHealthClient(connection).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	require.Error(t, err)
}

// waitProcessReady 等待指定真实进程路由可用，超时后打印所有相关进程日志。
func waitProcessReady(t *testing.T, suite *integration.Suite, path string, processes ...*integration.Suite) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		response, err := suite.DoJSON(http.MethodGet, path, "", nil)
		if err == nil && response.Success {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(processes) == 0 {
		suite.PrintLog()
	} else {
		for index, process := range processes {
			t.Logf("process %d log:", index)
			process.PrintLog()
		}
	}
	t.Fatalf("等待进程启动超时: %s", path)
}
