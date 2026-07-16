package config

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClusterConfigApplyDefaults_EmptyStruct 验证零值结构体补充后所有默认值正确。
func TestClusterConfigApplyDefaults_EmptyStruct(t *testing.T) {
	var c ClusterConfig
	c.ApplyDefaults()

	assert.Equal(t, "auto", c.Mode)
	assert.Equal(t, "local", c.Provider)
	assert.Equal(t, 3*time.Second, c.HeartbeatInterval)
	assert.Equal(t, 10*time.Second, c.HeartbeatTimeout)
	assert.Equal(t, 15*time.Second, c.SuspectTimeout)
	assert.Equal(t, 30*time.Second, c.InstanceReuseCooldown)
	assert.Equal(t, "expand-machine-id", c.Claim.ConflictPolicy)
	assert.Equal(t, uint(31), c.Claim.MachineIDMax)
	assert.Equal(t, uint(31), c.Claim.DataCenterIDMax)
	assert.Equal(t, "error", c.Shard.MissingKeyPolicy)
	assert.Equal(t, "error", c.Shard.EmptyCandidatePolicy)
	assert.NotNil(t, c.Services)
	assert.Equal(t, "/core/cluster", c.Providers.Etcd.Prefix)
}

// TestClusterConfigApplyDefaults_PreserveExistingValues 验证已设置的值不被覆盖。
func TestClusterConfigApplyDefaults_PreserveExistingValues(t *testing.T) {
	c := ClusterConfig{
		Mode:              "on",
		Provider:          "etcd",
		HeartbeatInterval: 5 * time.Second,
	}
	c.ApplyDefaults()

	assert.Equal(t, "on", c.Mode)
	assert.Equal(t, "etcd", c.Provider)
	assert.Equal(t, 5*time.Second, c.HeartbeatInterval)
	// 未设置的字段应补默认值
	assert.Equal(t, 10*time.Second, c.HeartbeatTimeout)
}

// TestClusterConfigApplyDefaults_OldJSON 模拟旧配置 JSON 不含 Cluster 字段时解析后补默认值。
func TestClusterConfigApplyDefaults_OldJSON(t *testing.T) {
	oldJSON := `{}`
	var c ClusterConfig
	require.NoError(t, json.Unmarshal([]byte(oldJSON), &c))
	assert.NotPanics(t, func() { c.ApplyDefaults() })
	assert.Equal(t, "auto", c.Mode)
	assert.Equal(t, "local", c.Provider)
	assert.Equal(t, 3*time.Second, c.HeartbeatInterval)
	assert.NotNil(t, c.Services)
}

// TestClusterConfigValidate_ValidModes 合法 mode+provider 不返回 error（需先 ApplyDefaults）。
func TestClusterConfigValidate_ValidModes(t *testing.T) {
	for _, mode := range []string{"off", "auto", "on"} {
		c := ClusterConfig{Mode: mode, Provider: "local"}
		c.ApplyDefaults()
		c.Mode = mode // ApplyDefaults 不覆盖已有值
		assert.NoError(t, c.Validate(), "mode=%s", mode)
	}
}

// TestClusterConfigValidate_InvalidMode 非法 mode 返回 error。
func TestClusterConfigValidate_InvalidMode(t *testing.T) {
	c := ClusterConfig{Mode: "invalid", Provider: "local"}
	c.ApplyDefaults()
	c.Mode = "invalid" // 恢复为非法值（ApplyDefaults 不覆盖已有值）
	assert.Error(t, c.Validate())
}

// TestClusterConfigValidate_InvalidProvider 非法 provider 返回 error。
func TestClusterConfigValidate_InvalidProvider(t *testing.T) {
	c := ClusterConfig{Mode: "auto", Provider: "zookeeper"}
	c.ApplyDefaults()
	c.Provider = "zookeeper"
	assert.Error(t, c.Validate())
}

// TestClusterConfigValidate_EtcdRequiresEndpoints Mode=on + Provider=etcd 但无 Endpoints 时报错。
func TestClusterConfigValidate_EtcdRequiresEndpoints(t *testing.T) {
	c := ClusterConfig{Mode: "on", Provider: "etcd"}
	c.ApplyDefaults()
	c.Mode = "on"
	c.Provider = "etcd"

	assert.Error(t, c.Validate())

	c.Providers.Etcd.Endpoints = []string{"127.0.0.1:2379"}
	assert.NoError(t, c.Validate())
}

// TestClusterConfigValidate_ConsulRequiresAddress Mode=on + Provider=consul 但无 Address 时报错。
func TestClusterConfigValidate_ConsulRequiresAddress(t *testing.T) {
	c := ClusterConfig{Mode: "on", Provider: "consul"}
	c.ApplyDefaults()
	c.Mode = "on"
	c.Provider = "consul"

	assert.Error(t, c.Validate())

	c.Providers.Consul.Address = "127.0.0.1:8500"
	assert.NoError(t, c.Validate())
}

// TestClusterConfigValidate_InvalidConflictPolicy 非法 ConflictPolicy 返回 error。
func TestClusterConfigValidate_InvalidConflictPolicy(t *testing.T) {
	c := ClusterConfig{Mode: "auto", Provider: "local"}
	c.ApplyDefaults()
	c.Claim.ConflictPolicy = "bad-policy"
	assert.Error(t, c.Validate())
}

func TestClusterConfigValidate_ModeOffPreservesLegacyFields(t *testing.T) {
	c := ClusterConfig{
		Mode:     "off",
		Provider: "legacy-provider",
		NodeName: "legacy-node",
		Discovery: ClusterDiscoveryConfig{
			Seeds: []string{"legacy:1234"},
		},
		Services: map[string]ClusterServiceConfig{"legacy": {}},
	}

	assert.NoError(t, c.Validate())
}

func TestClusterConfigValidate_RejectedErrorsIncludeValue(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*ClusterConfig)
		want      string
	}{
		{name: "mode", configure: func(c *ClusterConfig) { c.Mode = "legacy" }, want: `"legacy"`},
		{name: "provider", configure: func(c *ClusterConfig) { c.Provider = "zookeeper" }, want: `"zookeeper"`},
		{name: "node name", configure: func(c *ClusterConfig) { c.NodeName = "node-a" }, want: `"node-a"`},
		{name: "auto machine id", configure: func(c *ClusterConfig) { c.Claim.AutoMachineID = true }, want: "true"},
		{name: "discovery seeds", configure: func(c *ClusterConfig) { c.Discovery.Seeds = []string{"node-a:9000"} }, want: "node-a:9000"},
		{name: "shard policy", configure: func(c *ClusterConfig) { c.Shard.MissingKeyPolicy = "average" }, want: `"average"`},
		{name: "consul prefix", configure: func(c *ClusterConfig) { c.Providers.Consul.Prefix = "custom" }, want: `"custom"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg ClusterConfig
			cfg.ApplyDefaults()
			tt.configure(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestClusterConfigValidate_ModeIsCheckedBeforeOffShortCircuit(t *testing.T) {
	err := (&ClusterConfig{Mode: "invalid", Provider: "legacy-provider"}).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster.mode")
}

func TestClusterConfigValidate_UnimplementedClusterFields(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*ClusterConfig)
		fieldPath string
	}{
		{name: "node name", configure: func(c *ClusterConfig) { c.NodeName = "node-a" }, fieldPath: "cluster.nodeName"},
		{name: "auto machine id", configure: func(c *ClusterConfig) { c.Claim.AutoMachineID = true }, fieldPath: "cluster.claim.autoMachineID"},
		{name: "auto data center id", configure: func(c *ClusterConfig) { c.Claim.AutoDataCenterID = true }, fieldPath: "cluster.claim.autoDataCenterID"},
		{name: "expand data center conflict", configure: func(c *ClusterConfig) { c.Claim.ConflictPolicy = "expand-data-center-id" }, fieldPath: "cluster.claim.conflictPolicy"},
		{name: "fail conflict", configure: func(c *ClusterConfig) { c.Claim.ConflictPolicy = "fail" }, fieldPath: "cluster.claim.conflictPolicy"},
		{name: "discovery seeds", configure: func(c *ClusterConfig) { c.Discovery.Seeds = []string{"node-a:9000"} }, fieldPath: "cluster.discovery.seeds"},
		{name: "discovery multicast", configure: func(c *ClusterConfig) { c.Discovery.Multicast = true }, fieldPath: "cluster.discovery.multicast"},
		{name: "discovery mdns", configure: func(c *ClusterConfig) { c.Discovery.MDNS = true }, fieldPath: "cluster.discovery.mdns"},
		{name: "shard key priority", configure: func(c *ClusterConfig) { c.Shard.KeyPriority = []string{"tenant"} }, fieldPath: "cluster.shard.keyPriority"},
		{name: "shard missing key policy", configure: func(c *ClusterConfig) { c.Shard.MissingKeyPolicy = "average" }, fieldPath: "cluster.shard.missingKeyPolicy"},
		{name: "shard empty candidate policy", configure: func(c *ClusterConfig) { c.Shard.EmptyCandidatePolicy = "average" }, fieldPath: "cluster.shard.emptyCandidatePolicy"},
		{name: "services", configure: func(c *ClusterConfig) { c.Services = map[string]ClusterServiceConfig{"orders": {}} }, fieldPath: "cluster.services"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ClusterConfig{Mode: "auto", Provider: "local"}
			tt.configure(&c)
			err := c.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.fieldPath)
			assert.Contains(t, err.Error(), "not implemented")
		})
	}
}

func TestClusterConfigValidate_NonConfigurableFields(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*ClusterConfig)
		fieldPath string
	}{
		{name: "heartbeat timeout", configure: func(c *ClusterConfig) { c.HeartbeatTimeout = 11 * time.Second }, fieldPath: "cluster.heartbeatTimeout"},
		{name: "suspect timeout", configure: func(c *ClusterConfig) { c.SuspectTimeout = 16 * time.Second }, fieldPath: "cluster.suspectTimeout"},
		{name: "reuse cooldown", configure: func(c *ClusterConfig) { c.InstanceReuseCooldown = 31 * time.Second }, fieldPath: "cluster.instanceReuseCooldown"},
		{name: "data center max", configure: func(c *ClusterConfig) { c.Claim.DataCenterIDMax = 63 }, fieldPath: "cluster.claim.dataCenterIDMax"},
		{name: "consul prefix", configure: func(c *ClusterConfig) { c.Providers.Consul.Prefix = "custom" }, fieldPath: "cluster.providers.consul.prefix"},
		{name: "consul ttl", configure: func(c *ClusterConfig) { c.Providers.Consul.TTL = 11 * time.Second }, fieldPath: "cluster.providers.consul.ttl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := ClusterConfig{Mode: "auto", Provider: "local"}
			tt.configure(&c)
			err := c.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.fieldPath)
			assert.Contains(t, err.Error(), "not configurable")
		})
	}
}

func TestClusterConfigValidate_CustomEtcdPrefix(t *testing.T) {
	c := ClusterConfig{Mode: "auto", Provider: "etcd"}
	c.Providers.Etcd.Prefix = "/tenant-a/discovery"

	assert.NoError(t, c.Validate())
}

func TestClusterConfigValidate_ImplementedConfigurationRemainsSupported(t *testing.T) {
	c := ClusterConfig{
		Mode:                  "auto",
		Provider:              "etcd",
		HeartbeatInterval:     5 * time.Second,
		HeartbeatTimeout:      DefaultClusterHeartbeatTimeout,
		SuspectTimeout:        DefaultClusterSuspectTimeout,
		InstanceReuseCooldown: DefaultClusterInstanceReuseCooldown,
		Claim: ClusterClaimConfig{
			MachineIDMax:    63,
			DataCenterIDMax: 31,
			ConflictPolicy:  "expand-machine-id",
		},
		Providers: ClusterProviderConfig{
			Etcd: EtcdProviderConfig{
				Endpoints: []string{"127.0.0.1:2379"},
				Prefix:    DefaultClusterEtcdPrefix,
				TTL:       20 * time.Second,
			},
			Consul: ConsulProviderConfig{
				Address: "127.0.0.1:8500",
				Prefix:  DefaultClusterConsulPrefix,
				TTL:     DefaultClusterProviderTTL,
			},
		},
	}

	assert.NoError(t, c.Validate())
}

// TestTransportConfigApplyDefaults_EmptyStruct 验证传输配置默认值。
func TestTransportConfigApplyDefaults_EmptyStruct(t *testing.T) {
	var tr TransportConfig
	tr.ApplyDefaults()

	assert.Equal(t, "grpc", tr.Internal)
	assert.Equal(t, []string{"grpc", "http", "socket"}, tr.Fallback)
	assert.Equal(t, 19090, tr.GRPC.Port)
	assert.Equal(t, 4*1024*1024, tr.GRPC.MaxRecvMsgSize)
	assert.Equal(t, 4*1024*1024, tr.GRPC.MaxSendMsgSize)
}

// TestTransportConfigValidate_ValidInternal 已实现的 internal 不报错。
func TestTransportConfigValidate_ValidInternal(t *testing.T) {
	for _, name := range []string{"grpc", "http", "socket"} {
		tr := TransportConfig{Internal: name}
		assert.NoError(t, tr.Validate(), "internal=%s", name)
	}
}

func TestTransportConfigValidate_UnimplementedInternal(t *testing.T) {
	for _, name := range []string{"quic", "mq"} {
		t.Run(name, func(t *testing.T) {
			err := (&TransportConfig{Internal: name}).Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "transport.internal")
			assert.Contains(t, err.Error(), "not implemented")
		})
	}
}

func TestTransportConfigValidate_UnimplementedFallback(t *testing.T) {
	for _, name := range []string{"quic", "mq"} {
		t.Run(name, func(t *testing.T) {
			err := (&TransportConfig{Internal: "grpc", Fallback: []string{"http", name}}).Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "transport.fallback")
			assert.Contains(t, err.Error(), "not implemented")
		})
	}
}

func TestTransportConfigValidate_UnimplementedQUICConfig(t *testing.T) {
	tests := []struct {
		name      string
		quic      QUICTransportConfig
		fieldPath string
	}{
		{name: "enable", quic: QUICTransportConfig{Enable: true}, fieldPath: "transport.quic.enable"},
		{name: "cert file", quic: QUICTransportConfig{CertFile: "server.crt"}, fieldPath: "transport.quic.certFile"},
		{name: "key file", quic: QUICTransportConfig{KeyFile: "server.key"}, fieldPath: "transport.quic.keyFile"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&TransportConfig{Internal: "grpc", QUIC: tt.quic}).Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.fieldPath)
			assert.Contains(t, err.Error(), "not implemented")
		})
	}
}

func TestTransportConfigValidate_UnimplementedEnableFields(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*TransportConfig)
		fieldPath string
	}{
		{name: "http", configure: func(tr *TransportConfig) { tr.HTTP.Enable = true }, fieldPath: "transport.http.enable"},
		{name: "socket", configure: func(tr *TransportConfig) { tr.Socket.Enable = true }, fieldPath: "transport.socket.enable"},
		{name: "grpc", configure: func(tr *TransportConfig) { tr.GRPC.Enable = true }, fieldPath: "transport.grpc.enable"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tr TransportConfig
			tt.configure(&tr)

			err := tr.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.fieldPath)
			assert.Contains(t, err.Error(), "not implemented")
			assert.Contains(t, err.Error(), "Internal/Fallback")
		})
	}
}

func TestTransportConfigValidate_GRPCPortNotConfigurable(t *testing.T) {
	for _, port := range []int{-1, 1, 19089, 19091, 65535, 65536} {
		t.Run(fmt.Sprintf("port_%d", port), func(t *testing.T) {
			err := (&TransportConfig{GRPC: GRPCTransportConfig{Port: port}}).Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "transport.grpc.port")
			assert.Contains(t, err.Error(), "not configurable")
		})
	}
}

func TestTransportConfigValidate_DefaultAndConfigurableMessageSizes(t *testing.T) {
	var defaults TransportConfig
	defaults.ApplyDefaults()
	require.NoError(t, defaults.Validate())

	configured := TransportConfig{
		GRPC: GRPCTransportConfig{
			Port:           19090,
			MaxRecvMsgSize: 8 * 1024 * 1024,
			MaxSendMsgSize: 16 * 1024 * 1024,
		},
	}
	configured.ApplyDefaults()
	assert.Equal(t, 8*1024*1024, configured.GRPC.MaxRecvMsgSize)
	assert.Equal(t, 16*1024*1024, configured.GRPC.MaxSendMsgSize)
	assert.NoError(t, configured.Validate())
}

// TestTransportConfigValidate_InvalidInternal 非法 internal 报错。
func TestTransportConfigValidate_InvalidInternal(t *testing.T) {
	tr := TransportConfig{Internal: "tcp"}
	assert.Error(t, tr.Validate())
}

// TestTransportConfigValidate_InvalidFallback fallback 中含非法值报错。
func TestTransportConfigValidate_InvalidFallback(t *testing.T) {
	tr := TransportConfig{Internal: "grpc", Fallback: []string{"grpc", "tcp"}}
	assert.Error(t, tr.Validate())
}
