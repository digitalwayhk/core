package grpc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sync/singleflight"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/digitalwayhk/core/pkg/server/config"
	pb "github.com/digitalwayhk/core/pkg/server/transport/grpc/proto"
	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/zrpc"
)

type testCertificateFiles struct {
	caFile         string
	serverCertFile string
	serverKeyFile  string
	clientCertFile string
	clientKeyFile  string
}

func TestClientSecurityOptions_InsecureAndMeshDoNotAddApplicationCredentials(t *testing.T) {
	for _, mode := range []string{"insecure", "mesh"} {
		t.Run(mode, func(t *testing.T) {
			options, err := clientSecurityOptions(config.GRPCSecurityConfig{Mode: mode})
			require.NoError(t, err)
			assert.Empty(t, options)
		})
	}
}

func TestLoadClientTLSConfig_ReportsStableFileErrors(t *testing.T) {
	_, err := loadClientTLSConfig(config.GRPCSecurityConfig{
		Mode: "mtls", CAFile: filepath.Join(t.TempDir(), "missing-ca.pem"),
		CertFile: "missing-cert.pem", KeyFile: "missing-key.pem",
	})
	require.ErrorContains(t, err, "Transport.GRPC.Security.CAFile")

	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, []byte("not a certificate"), 0o600))
	_, err = loadClientTLSConfig(config.GRPCSecurityConfig{
		Mode: "mtls", CAFile: caFile, CertFile: "missing-cert.pem", KeyFile: "missing-key.pem",
	})
	require.ErrorContains(t, err, "Transport.GRPC.Security.CAFile")

	files := createTestCertificateFiles(t)
	_, err = loadClientTLSConfig(config.GRPCSecurityConfig{
		Mode: "tls", CAFile: files.caFile, CertFile: filepath.Join(t.TempDir(), "missing-cert.pem"), KeyFile: files.clientKeyFile,
	})
	require.ErrorContains(t, err, "Transport.GRPC.Security.CertFile")
	_, err = loadClientTLSConfig(config.GRPCSecurityConfig{
		Mode: "tls", CAFile: files.caFile, CertFile: files.clientCertFile, KeyFile: filepath.Join(t.TempDir(), "missing-key.pem"),
	})
	require.ErrorContains(t, err, "Transport.GRPC.Security.KeyFile")
}

func TestGRPCTransport_TLSAndMTLSHandshake(t *testing.T) {
	files := createTestCertificateFiles(t)
	for _, tc := range []struct {
		name        string
		mode        string
		requireMTLS bool
	}{
		{name: "tls", mode: "tls"},
		{name: "mtls", mode: "mtls", requireMTLS: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr, stop := startSecureTestServer(t, files, tc.requireMTLS)
			defer stop()
			transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
				Mode: tc.mode, CAFile: files.caFile, CertFile: files.clientCertFile,
				KeyFile: files.clientKeyFile, ServerName: "core.test",
			}})
			defer transport.Stop(context.Background())
			require.NoError(t, transport.Health(context.Background(), addr))
			result, err := transport.Send(context.Background(), &coretypes.PayLoad{TraceID: tc.name}, addr)
			require.NoError(t, err)
			assert.Equal(t, []byte("ok"), result)
		})
	}
}

func TestGRPCTransport_RejectsWrongServerName(t *testing.T) {
	files := createTestCertificateFiles(t)
	addr, stop := startSecureTestServer(t, files, false)
	defer stop()
	transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
		Mode: "tls", CAFile: files.caFile, CertFile: files.clientCertFile,
		KeyFile: files.clientKeyFile, ServerName: "wrong.test",
	}})
	defer transport.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.Error(t, transport.Health(ctx, addr))
}

func TestGRPCTransport_RejectsCertificateSignedByDifferentValidCA(t *testing.T) {
	serverFiles := createTestCertificateFiles(t)
	clientFiles := createTestCertificateFiles(t)
	addr, stop := startSecureTestServer(t, serverFiles, false)
	defer stop()
	newTransport := func() *GRPCTransport {
		return New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
			Mode: "tls", CAFile: clientFiles.caFile, CertFile: clientFiles.clientCertFile,
			KeyFile: clientFiles.clientKeyFile, ServerName: "core.test",
		}})
	}
	for _, call := range []struct {
		name string
		run  func(context.Context, *GRPCTransport) error
	}{
		{name: "health", run: func(ctx context.Context, transport *GRPCTransport) error {
			return transport.Health(ctx, addr)
		}},
		{name: "send", run: func(ctx context.Context, transport *GRPCTransport) error {
			_, err := transport.Send(ctx, &coretypes.PayLoad{}, addr)
			return err
		}},
	} {
		t.Run(call.name, func(t *testing.T) {
			transport := newTransport()
			defer transport.Stop(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := call.run(ctx, transport)
			require.ErrorContains(t, err, "certificate signed by unknown authority")
		})
	}
}

func TestServerSecurity_InsecureAndMeshDoNotAddApplicationCredentials(t *testing.T) {
	for _, mode := range []string{"insecure", "mesh"} {
		t.Run(mode, func(t *testing.T) {
			options, err := serverSecurityOptions(config.GRPCSecurityConfig{Mode: mode})
			require.NoError(t, err)
			assert.Empty(t, options)
		})
	}
}

func TestServerSecurity_RejectsInvalidFilesDuringConstruction(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.pem")

	server, err := NewServer("127.0.0.1:0", config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
		Mode: "tls", CertFile: missing, KeyFile: missing,
	}}, echoHandler)
	require.ErrorContains(t, err, "Transport.GRPC.Security.CertFile")
	assert.Nil(t, server)

	files := createTestCertificateFiles(t)
	server, err = NewServer("127.0.0.1:0", config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
		Mode: "tls", CertFile: files.serverCertFile, KeyFile: missing,
	}}, echoHandler)
	require.ErrorContains(t, err, "Transport.GRPC.Security.KeyFile")
	assert.Nil(t, server)

	badCA := filepath.Join(dir, "bad-ca.pem")
	require.NoError(t, os.WriteFile(badCA, []byte("not a certificate"), 0o600))
	server, err = NewServer("127.0.0.1:0", config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
		Mode: "mtls", CAFile: badCA, CertFile: files.serverCertFile, KeyFile: files.serverKeyFile,
	}}, echoHandler)
	require.ErrorContains(t, err, "Transport.GRPC.Security.CAFile")
	assert.Nil(t, server)
}

func TestServerTLS_HandshakeAndTrustFailures(t *testing.T) {
	serverFiles := createTestCertificateFiles(t)
	otherFiles := createTestCertificateFiles(t)
	server, startResult := startConfiguredServer(t, config.GRPCSecurityConfig{
		Mode: "tls", CertFile: serverFiles.serverCertFile, KeyFile: serverFiles.serverKeyFile,
	})
	defer func() {
		server.Stop()
		require.NoError(t, <-startResult)
	}()

	for _, tc := range []struct {
		name       string
		caFile     string
		serverName string
		wantError  bool
	}{
		{name: "valid", caFile: serverFiles.caFile, serverName: "core.test"},
		{name: "wrong-server-name", caFile: serverFiles.caFile, serverName: "wrong.test", wantError: true},
		{name: "wrong-ca", caFile: otherFiles.caFile, serverName: "core.test", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
				Mode: "tls", CAFile: tc.caFile, CertFile: serverFiles.clientCertFile,
				KeyFile: serverFiles.clientKeyFile, ServerName: tc.serverName,
			}})
			defer transport.Stop(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			err := transport.Health(ctx, server.Address())
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestServerMTLS_RequiresAndAcceptsClientCertificate(t *testing.T) {
	files := createTestCertificateFiles(t)
	server, startResult := startConfiguredServer(t, config.GRPCSecurityConfig{
		Mode: "mtls", CAFile: files.caFile, CertFile: files.serverCertFile, KeyFile: files.serverKeyFile,
	})
	defer func() {
		server.Stop()
		require.NoError(t, <-startResult)
	}()

	rootCAs, err := loadRequiredCertPool(files.caFile)
	require.NoError(t, err)
	withoutCertificate, err := googlegrpc.NewClient(server.Address(), googlegrpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12, RootCAs: rootCAs, ServerName: "core.test",
	})))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, err = grpc_health_v1.NewHealthClient(withoutCertificate).Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	cancel()
	require.Error(t, err)
	require.NoError(t, withoutCertificate.Close())

	withCertificate := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{
		Mode: "mtls", CAFile: files.caFile, CertFile: files.clientCertFile,
		KeyFile: files.clientKeyFile, ServerName: "core.test",
	}})
	defer withCertificate.Stop(context.Background())
	require.NoError(t, withCertificate.Health(context.Background(), server.Address()))
}

func startConfiguredServer(t *testing.T, security config.GRPCSecurityConfig) (*Server, <-chan error) {
	t.Helper()
	server, err := NewServer("127.0.0.1:0", config.GRPCTransportConfig{Security: security}, echoHandler)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- server.Start() }()
	waitReady(t, server)
	return server, result
}

func TestGRPCTransport_ConcurrentInitializationCallsFactoryOnce(t *testing.T) {
	const workers = 100
	transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}})
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int64
	var conn *googlegrpc.ClientConn
	transport.newClient = func(conf zrpc.RpcClientConf, _ ...zrpc.ClientOption) (zrpc.Client, error) {
		calls.Add(1)
		assert.Equal(t, []string{"127.0.0.1:19090"}, conf.Endpoints)
		assert.True(t, conf.NonBlock)
		assert.Equal(t, int64(2000), conf.Timeout)
		assert.True(t, conf.Middlewares.Trace)
		assert.True(t, conf.Middlewares.Duration)
		assert.True(t, conf.Middlewares.Prometheus)
		assert.True(t, conf.Middlewares.Breaker)
		assert.True(t, conf.Middlewares.Timeout)
		created, err := googlegrpc.NewClient("passthrough:///unused", googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		conn = created
		entered <- struct{}{}
		<-release
		return testZRPCClient{conn: created}, nil
	}

	results := make([]<-chan singleflight.Result, 0, workers)
	results = append(results, transport.initializeClient("127.0.0.1:19090"))
	<-entered
	joined := make(chan (<-chan singleflight.Result), workers-1)
	for i := 1; i < workers; i++ {
		go func() { joined <- transport.initializeClient("127.0.0.1:19090") }()
	}
	for i := 1; i < workers; i++ {
		results = append(results, <-joined)
	}
	close(release)
	for _, result := range results {
		initialized := <-result
		require.NoError(t, initialized.Err)
		require.NotNil(t, initialized.Val)
	}
	assert.Equal(t, int64(1), calls.Load())
	assert.Equal(t, 1, transport.PooledConns())
	require.NoError(t, transport.Stop(context.Background()))
	assert.Equal(t, connectivity.Shutdown, conn.GetState())
}

func TestGRPCTransport_ConcurrentSendCallsZRPCFactoryExactlyOnce(t *testing.T) {
	addr, stop := startInsecureCoreTestServer(t)
	defer stop()
	transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}})
	defer transport.Stop(context.Background())
	var calls atomic.Int64
	transport.newClient = func(conf zrpc.RpcClientConf, options ...zrpc.ClientOption) (zrpc.Client, error) {
		calls.Add(1)
		return zrpc.NewClient(conf, options...)
	}

	start := make(chan struct{})
	errs := make(chan error, 100)
	var workers sync.WaitGroup
	workers.Add(100)
	for i := 0; i < 100; i++ {
		go func() {
			defer workers.Done()
			<-start
			_, err := transport.Send(context.Background(), &coretypes.PayLoad{TraceID: "singleflight"}, addr)
			errs <- err
		}()
	}
	close(start)
	workers.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int64(1), calls.Load())
	assert.Equal(t, 1, transport.PooledConns())
}

func TestGRPCTransport_ConcurrentInitializationSharesFailureAndLaterRetries(t *testing.T) {
	const workers = 100
	transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}})
	wantErr := errors.New("dial failed")
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int64
	transport.newClient = func(zrpc.RpcClientConf, ...zrpc.ClientOption) (zrpc.Client, error) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		return nil, wantErr
	}

	results := make([]<-chan singleflight.Result, 0, workers)
	results = append(results, transport.initializeClient("127.0.0.1:19090"))
	<-entered
	joined := make(chan (<-chan singleflight.Result), workers-1)
	for i := 1; i < workers; i++ {
		go func() { joined <- transport.initializeClient("127.0.0.1:19090") }()
	}
	for i := 1; i < workers; i++ {
		results = append(results, <-joined)
	}
	close(release)
	for _, result := range results {
		initialized := <-result
		require.ErrorIs(t, initialized.Err, wantErr)
		assert.Nil(t, initialized.Val)
	}
	assert.Equal(t, int64(1), calls.Load())
	assert.Zero(t, transport.PooledConns())

	conn, err := googlegrpc.NewClient("passthrough:///unused", googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	transport.newClient = func(zrpc.RpcClientConf, ...zrpc.ClientOption) (zrpc.Client, error) {
		calls.Add(1)
		return testZRPCClient{conn: conn}, nil
	}
	client, err := transport.getClient("127.0.0.1:19090")
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, int64(2), calls.Load())
	require.NoError(t, transport.Stop(context.Background()))
}

func TestGRPCTransport_DifferentEndpointsInitializeInParallel(t *testing.T) {
	transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}})
	entered := make(chan string, 2)
	release := make(chan struct{})
	transport.newClient = func(conf zrpc.RpcClientConf, _ ...zrpc.ClientOption) (zrpc.Client, error) {
		entered <- conf.Endpoints[0]
		<-release
		conn, err := googlegrpc.NewClient("passthrough:///unused", googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
		return testZRPCClient{conn: conn}, err
	}
	first := transport.initializeClient("127.0.0.1:19090")
	second := transport.initializeClient("127.0.0.1:19091")
	seen := map[string]bool{<-entered: true, <-entered: true}
	assert.True(t, seen["127.0.0.1:19090"])
	assert.True(t, seen["127.0.0.1:19091"])
	close(release)
	require.NoError(t, (<-first).Err)
	require.NoError(t, (<-second).Err)
	assert.Equal(t, 2, transport.PooledConns())
	require.NoError(t, transport.Stop(context.Background()))
}

func TestGRPCTransport_EmptyEndpointFailsBeforeClientCreation(t *testing.T) {
	transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}})
	var calls atomic.Int64
	transport.newClient = func(zrpc.RpcClientConf, ...zrpc.ClientOption) (zrpc.Client, error) {
		calls.Add(1)
		return nil, errors.New("factory must not be called")
	}
	client, err := transport.getClient("  ")
	require.ErrorIs(t, err, errEmptyEndpoint)
	assert.Nil(t, client)
	assert.Zero(t, calls.Load())
}

func TestGRPCTransport_StopWaitsForConcurrentClientCreationAndLeavesPoolEmpty(t *testing.T) {
	transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}})
	entered := make(chan struct{})
	release := make(chan struct{})
	conn, err := googlegrpc.NewClient("passthrough:///unused", googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	transport.newClient = func(zrpc.RpcClientConf, ...zrpc.ClientOption) (zrpc.Client, error) {
		close(entered)
		<-release
		return testZRPCClient{conn: conn}, nil
	}

	clientDone := make(chan error, 1)
	go func() {
		_, getErr := transport.getClient("127.0.0.1:19090")
		clientDone <- getErr
	}()
	<-entered
	stopDone := make(chan error, 1)
	go func() { stopDone <- transport.Stop(context.Background()) }()
	close(release)
	require.NoError(t, <-clientDone)
	require.NoError(t, <-stopDone)
	assert.Zero(t, transport.PooledConns())
	assert.Equal(t, connectivity.Shutdown, conn.GetState())
	_, err = transport.getClient("127.0.0.1:19090")
	require.ErrorIs(t, err, errTransportStopped)
}

type testZRPCClient struct {
	conn *googlegrpc.ClientConn
}

func (c testZRPCClient) Conn() *googlegrpc.ClientConn { return c.conn }

func startInsecureCoreTestServer(t *testing.T) (string, func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := googlegrpc.NewServer()
	pb.RegisterCoreTransportServer(server, &secureTestCoreServer{})
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	go server.Serve(lis)
	return lis.Addr().String(), server.GracefulStop
}

func startSecureTestServer(t *testing.T, files testCertificateFiles, requireMTLS bool) (string, func()) {
	t.Helper()
	certificate, err := tls.LoadX509KeyPair(files.serverCertFile, files.serverKeyFile)
	require.NoError(t, err)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}
	if requireMTLS {
		caPEM, readErr := os.ReadFile(files.caFile)
		require.NoError(t, readErr)
		pool := x509.NewCertPool()
		require.True(t, pool.AppendCertsFromPEM(caPEM))
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		tlsConfig.ClientCAs = pool
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := googlegrpc.NewServer(googlegrpc.Creds(credentials.NewTLS(tlsConfig)))
	pb.RegisterCoreTransportServer(server, &secureTestCoreServer{})
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	go server.Serve(lis)
	return lis.Addr().String(), server.Stop
}

type secureTestCoreServer struct {
	pb.UnimplementedCoreTransportServer
}

func (*secureTestCoreServer) Call(context.Context, *pb.PayloadRequest) (*pb.PayloadResponse, error) {
	return &pb.PayloadResponse{Data: []byte("ok")}, nil
}

func createTestCertificateFiles(t *testing.T) testCertificateFiles {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caFile := filepath.Join(dir, "ca.pem")
	writePEMFile(t, caFile, "CERTIFICATE", caDER)

	serverCert, serverKey := createSignedCertificate(t, caTemplate, caKey, 2, "core.test", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})
	clientCert, clientKey := createSignedCertificate(t, caTemplate, caKey, 3, "client.test", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})
	files := testCertificateFiles{
		caFile: caFile, serverCertFile: filepath.Join(dir, "server.pem"), serverKeyFile: filepath.Join(dir, "server-key.pem"),
		clientCertFile: filepath.Join(dir, "client.pem"), clientKeyFile: filepath.Join(dir, "client-key.pem"),
	}
	writePEMFile(t, files.serverCertFile, "CERTIFICATE", serverCert)
	writePrivateKey(t, files.serverKeyFile, serverKey)
	writePEMFile(t, files.clientCertFile, "CERTIFICATE", clientCert)
	writePrivateKey(t, files.clientKeyFile, clientKey)
	return files
}

func createSignedCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, serial int64, commonName string, usages []x509.ExtKeyUsage) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: commonName},
		DNSNames: []string{commonName}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: usages,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	return der, key
}

func writePrivateKey(t *testing.T, path string, key *rsa.PrivateKey) {
	t.Helper()
	writePEMFile(t, path, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key))
}

func writePEMFile(t *testing.T, path, blockType string, data []byte) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: data}), 0o600))
}
