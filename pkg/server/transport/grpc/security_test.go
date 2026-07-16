package grpc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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

func TestGRPCTransport_LoadOrStoreClosesLosingZRPCClients(t *testing.T) {
	const workers = 32
	transport := New(config.GRPCTransportConfig{Security: config.GRPCSecurityConfig{Mode: "insecure"}})
	entered := make(chan struct{}, workers)
	release := make(chan struct{})
	var connsMu sync.Mutex
	var conns []*googlegrpc.ClientConn
	transport.newClient = func(conf zrpc.RpcClientConf, _ ...zrpc.ClientOption) (zrpc.Client, error) {
		assert.Equal(t, []string{"127.0.0.1:19090"}, conf.Endpoints)
		assert.True(t, conf.NonBlock)
		assert.Equal(t, int64(2000), conf.Timeout)
		assert.True(t, conf.Middlewares.Trace)
		assert.True(t, conf.Middlewares.Duration)
		assert.True(t, conf.Middlewares.Prometheus)
		assert.True(t, conf.Middlewares.Breaker)
		assert.True(t, conf.Middlewares.Timeout)
		conn, err := googlegrpc.NewClient("passthrough:///unused", googlegrpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		connsMu.Lock()
		conns = append(conns, conn)
		connsMu.Unlock()
		entered <- struct{}{}
		<-release
		return testZRPCClient{conn: conn}, nil
	}

	errs := make(chan error, workers)
	var group sync.WaitGroup
	group.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer group.Done()
			_, err := transport.getClient("127.0.0.1:19090")
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		<-entered
	}
	close(release)
	group.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, conns, workers)
	assert.Equal(t, 1, transport.PooledConns())
	shutdown := 0
	for _, conn := range conns {
		if conn.GetState() == connectivity.Shutdown {
			shutdown++
		}
	}
	assert.Equal(t, workers-1, shutdown)
	require.NoError(t, transport.Stop(context.Background()))
	for _, conn := range conns {
		assert.Equal(t, connectivity.Shutdown, conn.GetState())
	}
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
