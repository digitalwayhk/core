package config

import (
	"fmt"
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
			cfg := externalServerConfig(provider)

			cfg.ApplyDefaults()

			assert.Equal(t, 18080, cfg.Transport.GRPC.Port)
			assert.Equal(t, "mtls", cfg.Transport.GRPC.Security.Mode)
			require.ErrorContains(t, cfg.Validate(), "Transport.GRPC.Security.CAFile")
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
	addresses := []string{
		"127.0.0.1",
		"127.0.0.1:18080",
		"::1",
		"[::1]:18080",
		"localhost",
		"localhost:18080",
	}
	for _, address := range addresses {
		t.Run(address, func(t *testing.T) {
			cfg := validExternalServerConfig()
			cfg.Cluster.AdvertiseAddress = address
			cfg.Transport.GRPC.Security.Mode = "insecure"
			cfg.ApplyDefaults()

			require.NoError(t, cfg.Validate())
		})
	}
}

func TestServerConfigExternalInsecureGRPCFallsBackToRunIP(t *testing.T) {
	tests := []struct {
		name    string
		runIP   string
		wantErr bool
	}{
		{name: "loopback", runIP: "127.0.0.1"},
		{name: "remote", runIP: "10.20.30.40", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validExternalServerConfig()
			cfg.Cluster.AdvertiseAddress = ""
			cfg.RunIp = tt.runIP
			cfg.Transport.GRPC.Security.Mode = "insecure"
			cfg.ApplyDefaults()

			err := cfg.Validate()
			if tt.wantErr {
				require.ErrorContains(t, err, "insecure grpc is limited to loopback")
				return
			}
			require.NoError(t, err)
		})
	}
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

func TestTransportConfigSecurityAcceptsCompleteTLSAndMTLS(t *testing.T) {
	tests := []GRPCSecurityConfig{
		{Mode: "tls", CertFile: "server.crt", KeyFile: "server.key", ServerName: "core.internal"},
		{Mode: "mtls", CAFile: "ca.crt", CertFile: "server.crt", KeyFile: "server.key", ServerName: "core.internal"},
	}
	for _, security := range tests {
		t.Run(security.Mode, func(t *testing.T) {
			tr := TransportConfig{GRPC: GRPCTransportConfig{Security: security}}
			require.NoError(t, tr.Validate())
		})
	}
}

func TestTransportConfigSecurityRejectsInvalidMode(t *testing.T) {
	tr := TransportConfig{GRPC: GRPCTransportConfig{Security: GRPCSecurityConfig{Mode: "plaintext"}}}

	err := tr.Validate()

	require.ErrorContains(t, err, "Transport.GRPC.Security.Mode")
}

func TestTransportConfigSecurityRequiresModeWhenFieldsConfigured(t *testing.T) {
	tests := []struct {
		name     string
		security GRPCSecurityConfig
	}{
		{name: "ca file", security: GRPCSecurityConfig{CAFile: "ca.crt"}},
		{name: "cert file", security: GRPCSecurityConfig{CertFile: "server.crt"}},
		{name: "key file", security: GRPCSecurityConfig{KeyFile: "server.key"}},
		{name: "server name", security: GRPCSecurityConfig{ServerName: "core.internal"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := TransportConfig{GRPC: GRPCTransportConfig{Security: tt.security}}

			err := tr.Validate()

			require.ErrorContains(t, err, "Transport.GRPC.Security.Mode")
			require.ErrorContains(t, err, "Mode is required when security fields are configured")
		})
	}
}

func TestTransportConfigSecurityAllowsEmptyModeWithoutFields(t *testing.T) {
	tr := TransportConfig{GRPC: GRPCTransportConfig{Security: GRPCSecurityConfig{}}}
	require.NoError(t, tr.Validate())
}

func TestServerConfigDoesNotInferGRPCSecurityModeWhenFieldsConfigured(t *testing.T) {
	environments := []struct {
		name string
		new  func() ServerConfig
	}{
		{name: "local", new: func() ServerConfig {
			cfg := ServerConfig{}
			cfg.Port = 8080
			return cfg
		}},
		{name: "cluster off", new: func() ServerConfig {
			cfg := ServerConfig{Cluster: ClusterConfig{Mode: "off", Provider: "redis"}}
			cfg.Port = 8080
			return cfg
		}},
		{name: "redis", new: func() ServerConfig { return externalServerConfig("redis") }},
		{name: "etcd", new: func() ServerConfig { return externalServerConfig("etcd") }},
		{name: "consul", new: func() ServerConfig { return externalServerConfig("consul") }},
	}
	fields := []struct {
		name     string
		security GRPCSecurityConfig
	}{
		{name: "ca file", security: GRPCSecurityConfig{CAFile: "ca.crt"}},
		{name: "cert file", security: GRPCSecurityConfig{CertFile: "server.crt"}},
		{name: "key file", security: GRPCSecurityConfig{KeyFile: "server.key"}},
		{name: "server name", security: GRPCSecurityConfig{ServerName: "core.internal"}},
	}
	for _, environment := range environments {
		for _, field := range fields {
			t.Run(environment.name+"/"+field.name, func(t *testing.T) {
				cfg := environment.new()
				cfg.Transport.GRPC.Security = field.security

				cfg.ApplyDefaults()

				assert.Empty(t, cfg.Transport.GRPC.Security.Mode)
				err := cfg.Validate()
				require.ErrorContains(t, err, "Transport.GRPC.Security.Mode")
				require.ErrorContains(t, err, "Mode is required when security fields are configured")
			})
		}
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

func TestTransportConfigGRPCPortBoundaries(t *testing.T) {
	tests := []struct {
		port    int
		wantErr bool
	}{
		{port: -1, wantErr: true},
		{port: 0},
		{port: 1},
		{port: 65535},
		{port: 65536, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("port_%d", tt.port), func(t *testing.T) {
			tr := TransportConfig{GRPC: GRPCTransportConfig{Port: tt.port}}
			err := tr.Validate()
			if tt.wantErr {
				require.ErrorContains(t, err, "Transport.GRPC.Port")
				require.ErrorContains(t, err, "must be 0 or between 1 and 65535")
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestTransportConfigApplyServerDefaultsSetsNonZeroGRPCPort(t *testing.T) {
	var tr TransportConfig
	require.NoError(t, tr.Validate())

	tr.ApplyServerDefaults(ClusterConfig{Mode: "off", Provider: "local"}, 8080)

	assert.Equal(t, 18080, tr.GRPC.Port)
}

func TestTransportConfigApplyServerDefaultsKeepsAutomaticPortForUnsafeDerivation(t *testing.T) {
	for _, httpPort := range []int{0, 60000} {
		t.Run(fmt.Sprintf("http_%d", httpPort), func(t *testing.T) {
			var tr TransportConfig
			tr.ApplyServerDefaults(ClusterConfig{Mode: "off", Provider: "local"}, httpPort)
			assert.Zero(t, tr.GRPC.Port)
		})
	}
}

func validExternalServerConfig() ServerConfig {
	return externalServerConfig("redis")
}

func externalServerConfig(provider string) ServerConfig {
	cfg := ServerConfig{
		RunIp: "10.20.30.40",
		Cluster: ClusterConfig{
			Mode:     "on",
			Provider: provider,
		},
	}
	cfg.Port = 8080
	switch provider {
	case "redis":
		cfg.Cluster.Providers.Redis.Addr = "127.0.0.1:6379"
	case "etcd":
		cfg.Cluster.Providers.Etcd.Endpoints = []string{"127.0.0.1:2379"}
	case "consul":
		cfg.Cluster.Providers.Consul.Address = "127.0.0.1:8500"
	}
	return cfg
}
