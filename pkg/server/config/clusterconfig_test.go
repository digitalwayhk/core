package config

import (
	"encoding/json"
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

// TestTransportConfigValidate_ValidInternal 合法 internal 不报错。
func TestTransportConfigValidate_ValidInternal(t *testing.T) {
	for _, name := range []string{"grpc", "http", "socket", "quic", "mq"} {
		tr := TransportConfig{Internal: name}
		assert.NoError(t, tr.Validate(), "internal=%s", name)
	}
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

// TestTransportConfigValidate_InvalidPort 非法端口报错。
func TestTransportConfigValidate_InvalidPort(t *testing.T) {
	tr := TransportConfig{GRPC: GRPCTransportConfig{Port: -1}}
	assert.Error(t, tr.Validate())
}

