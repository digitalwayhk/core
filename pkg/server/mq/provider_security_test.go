package mq

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildMQTLSConfig_DisabledReturnsNil 验证 TLS 默认关闭且不会构造宽松配置。
func TestBuildMQTLSConfig_DisabledReturnsNil(t *testing.T) {
	tlsConfig, err := buildMQTLSConfig(config.MQTLSConfig{})
	require.NoError(t, err)
	assert.Nil(t, tlsConfig)
}

// TestBuildMQTLSConfig_RejectsUnreadableCA 验证不可读 CA fail closed 且错误不泄露内容。
func TestBuildMQTLSConfig_RejectsUnreadableCA(t *testing.T) {
	tlsConfig, err := buildMQTLSConfig(config.MQTLSConfig{Enable: true, CAFile: filepath.Join(t.TempDir(), "missing-ca.pem")})
	require.Error(t, err)
	assert.Nil(t, tlsConfig)
	assert.Contains(t, err.Error(), "CAFile")
}

// TestBuildMQTLSConfig_RejectsInvalidCAWithoutEchoingContent 验证解析错误不回显证书内容。
func TestBuildMQTLSConfig_RejectsInvalidCAWithoutEchoingContent(t *testing.T) {
	const secretContent = "private-ca-content-must-not-leak"
	path := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(path, []byte(secretContent), 0o600))

	tlsConfig, err := buildMQTLSConfig(config.MQTLSConfig{Enable: true, CAFile: path})
	require.Error(t, err)
	assert.Nil(t, tlsConfig)
	assert.NotContains(t, err.Error(), secretContent)
}

// TestRedactedAMQPURL_RemovesUserInfo 验证 RabbitMQ URL 错误展示不会泄露用户名和密码。
func TestRedactedAMQPURL_RemovesUserInfo(t *testing.T) {
	redacted := redactedAMQPURL("amqps://alice:super-secret@rabbit.example.com:5671/vhost")
	assert.Equal(t, "amqps://rabbit.example.com:5671/vhost", redacted)
	assert.NotContains(t, redacted, "alice")
	assert.NotContains(t, redacted, "super-secret")
}
