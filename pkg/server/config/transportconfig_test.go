package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfigAppliesGRPCDefaultsForLocal(t *testing.T) {
	cfg := ServerConfig{}
	cfg.Port = 9090

	cfg.ApplyDefaults()

	assert.Equal(t, 19090, cfg.Transport.GRPC.Port)
	assert.Equal(t, "insecure", cfg.Transport.GRPC.Security.Mode)
}

func TestServerConfigAppliesGRPCDefaultsForClusterOff(t *testing.T) {
	cfg := ServerConfig{Cluster: ClusterConfig{Mode: "off", Provider: "redis"}}
	cfg.Port = 9090

	cfg.ApplyDefaults()

	assert.Equal(t, "insecure", cfg.Transport.GRPC.Security.Mode)
}

func TestServerConfigExternalDiscoveryDefaultsToMTLS(t *testing.T) {
	for _, provider := range []string{"redis", "etcd", "consul"} {
		t.Run(provider, func(t *testing.T) {
			cfg := ServerConfig{Cluster: ClusterConfig{Mode: "on", Provider: provider}}
			cfg.Port = 8080

			cfg.ApplyDefaults()

			assert.Equal(t, 18080, cfg.Transport.GRPC.Port)
			assert.Equal(t, "mtls", cfg.Transport.GRPC.Security.Mode)
		})
	}
}

func TestServerConfigMeshDoesNotRequireApplicationCertificates(t *testing.T) {
	cfg := validExternalServerConfig()
	cfg.Transport.GRPC.Security.Mode = "mesh"

	cfg.ApplyDefaults()

	require.NoError(t, cfg.Validate())
}

func TestServerConfigRejectsRemoteInsecureGRPC(t *testing.T) {
	cfg := validExternalServerConfig()
	cfg.RunIp = "127.0.0.1"
	cfg.Cluster.AdvertiseAddress = "10.20.30.40:18080"
	cfg.Transport.GRPC.Security.Mode = "insecure"
	cfg.ApplyDefaults()

	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insecure grpc is limited to loopback")
}

func TestServerConfigAllowsExternalInsecureGRPCOnLoopback(t *testing.T) {
	cfg := validExternalServerConfig()
	cfg.Cluster.AdvertiseAddress = "127.0.0.1:18080"
	cfg.Transport.GRPC.Security.Mode = "insecure"
	cfg.ApplyDefaults()

	require.NoError(t, cfg.Validate())
}

func TestServerConfigPreservesCustomGRPCPort(t *testing.T) {
	cfg := ServerConfig{}
	cfg.Port = 9090
	cfg.Transport.GRPC.Port = 25051

	cfg.ApplyDefaults()

	assert.Equal(t, 25051, cfg.Transport.GRPC.Port)
}

func TestTransportConfigSecurityRequiresTLSFiles(t *testing.T) {
	tests := []struct {
		name     string
		security GRPCSecurityConfig
		wantPath string
	}{
		{name: "tls cert", security: GRPCSecurityConfig{Mode: "tls", KeyFile: "server.key"}, wantPath: "Transport.GRPC.Security.CertFile"},
		{name: "tls key", security: GRPCSecurityConfig{Mode: "tls", CertFile: "server.crt"}, wantPath: "Transport.GRPC.Security.KeyFile"},
		{name: "mtls ca", security: GRPCSecurityConfig{Mode: "mtls", CertFile: "server.crt", KeyFile: "server.key"}, wantPath: "Transport.GRPC.Security.CAFile"},
		{name: "mtls cert", security: GRPCSecurityConfig{Mode: "mtls", CAFile: "ca.crt", KeyFile: "server.key"}, wantPath: "Transport.GRPC.Security.CertFile"},
		{name: "mtls key", security: GRPCSecurityConfig{Mode: "mtls", CAFile: "ca.crt", CertFile: "server.crt"}, wantPath: "Transport.GRPC.Security.KeyFile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := TransportConfig{GRPC: GRPCTransportConfig{Security: tt.security}}
			err := tr.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantPath)
		})
	}
}

func TestTransportConfigSecurityRejectsCertificatesForInsecureAndMesh(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		security GRPCSecurityConfig
		wantPath string
	}{
		{name: "insecure ca", mode: "insecure", security: GRPCSecurityConfig{CAFile: "ca.crt"}, wantPath: "Transport.GRPC.Security.CAFile"},
		{name: "insecure cert", mode: "insecure", security: GRPCSecurityConfig{CertFile: "server.crt"}, wantPath: "Transport.GRPC.Security.CertFile"},
		{name: "insecure key", mode: "insecure", security: GRPCSecurityConfig{KeyFile: "server.key"}, wantPath: "Transport.GRPC.Security.KeyFile"},
		{name: "insecure server name", mode: "insecure", security: GRPCSecurityConfig{ServerName: "core.internal"}, wantPath: "Transport.GRPC.Security.ServerName"},
		{name: "mesh ca", mode: "mesh", security: GRPCSecurityConfig{CAFile: "ca.crt"}, wantPath: "Transport.GRPC.Security.CAFile"},
		{name: "mesh cert", mode: "mesh", security: GRPCSecurityConfig{CertFile: "server.crt"}, wantPath: "Transport.GRPC.Security.CertFile"},
		{name: "mesh key", mode: "mesh", security: GRPCSecurityConfig{KeyFile: "server.key"}, wantPath: "Transport.GRPC.Security.KeyFile"},
		{name: "mesh server name", mode: "mesh", security: GRPCSecurityConfig{ServerName: "core.internal"}, wantPath: "Transport.GRPC.Security.ServerName"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.security.Mode = tt.mode
			tr := TransportConfig{GRPC: GRPCTransportConfig{Security: tt.security}}
			err := tr.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantPath)
		})
	}
}

func TestTransportConfigFallbackDefaultsToEmpty(t *testing.T) {
	var tr TransportConfig

	tr.ApplyDefaults()

	assert.Empty(t, tr.Fallback)
}

func validExternalServerConfig() ServerConfig {
	cfg := ServerConfig{
		RunIp: "10.20.30.40",
		Cluster: ClusterConfig{
			Mode:     "on",
			Provider: "redis",
			Providers: ClusterProviderConfig{
				Redis: RedisProviderConfig{Addr: "127.0.0.1:6379"},
			},
		},
	}
	cfg.Port = 8080
	return cfg
}
