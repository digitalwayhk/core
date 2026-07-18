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

func TestThreeProcessDiscoveryAndRemoteCalls(t *testing.T) {
	pki := integration.NewGRPCTestPKI(t, "shop-user", "shop-supplier", "shop-order")
	redisPrefix := "core:test:06:three-process:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	assertPKIFileModes(t, pki)
	assertPKISANs(t, pki)
	parentCA, parentCASet := os.LookupEnv("SHOP_GRPC_CA_FILE")
	user, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/06-shop-microservices/main/user", BinaryName: "shop-user", TempPrefix: "core-shop-user-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: processEnvironment(pki, "shop-user", redisPrefix)})
	require.NoError(t, err)
	defer user.Stop()
	assertParentEnvironmentUnchanged(t, "SHOP_GRPC_CA_FILE", parentCA, parentCASet)
	supplier, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/06-shop-microservices/main/supplier", BinaryName: "shop-supplier", TempPrefix: "core-shop-supplier-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: processEnvironment(pki, "shop-supplier", redisPrefix)})
	require.NoError(t, err)
	defer supplier.Stop()
	order, err := integration.StartProcess(integration.ProcessOptions{BuildPackage: "./examples/06-shop-microservices/main/order", BinaryName: "shop-order", TempPrefix: "core-shop-order-", ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2, Arguments: []string{"-view", "0"}, Environment: processEnvironment(pki, "shop-order", redisPrefix)})
	require.NoError(t, err)
	defer order.Stop()
	waitProcessReady(t, user, "/api/shop-user/getproducts")
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
	processes := []*integration.Suite{user, supplier, order}
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

func assertParentEnvironmentUnchanged(t *testing.T, key, before string, existed bool) {
	t.Helper()
	after, stillExists := os.LookupEnv(key)
	require.Equal(t, existed, stillExists)
	require.Equal(t, before, after)
}

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

func waitProcessReady(t *testing.T, suite *integration.Suite, path string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := suite.DoJSON(http.MethodGet, path, "", nil)
		if err == nil && response.Success {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	suite.PrintLog()
	t.Fatalf("等待进程启动超时: %s", path)
}
