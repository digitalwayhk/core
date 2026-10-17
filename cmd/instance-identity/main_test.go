// 本文件验证离线工具只能显式初始化，不能覆盖已有身份。
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/stretchr/testify/require"
)

// TestOfflineCreateAndAuthority 验证真实 CLI 流程和不可覆盖边界。
func TestOfflineCreateAndAuthority(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "instance")
	c := config.NewServiceDefaultConfig("orders", 18080)
	c.Cluster.Mode = "off"
	c.Cluster.IdentityMode = "stable"
	c.Cluster.IdentityFile = filepath.Join(stateDir, "identity.json")
	c.IDGenerator = &config.IDGeneratorConfig{Provider: "local", Namespace: "test", Epoch: "epoch-a", LocalPath: filepath.Join(root, "authority.json"), LocalDevelopment: true}
	data, err := json.Marshal(c)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	require.Error(t, run([]string{"init-authority", "--config", path, "--dry-run"}), "不能静默忽略 dry-run 后写入权威")
	require.NoFileExists(t, c.IDGenerator.LocalPath)
	require.Error(t, run([]string{"create", "--config", path, "--service", "orders", "--state-dir", stateDir, "--instance-id", "a"}))
	require.NoError(t, run([]string{"init-authority", "--config", path}))
	require.NoError(t, run([]string{"create", "--config", path, "--service", "orders", "--state-dir", stateDir, "--instance-id", "a"}))
	require.Error(t, run([]string{"create", "--config", path, "--service", "orders", "--state-dir", stateDir, "--instance-id", "b"}))
	f, err := identity.Open(c.Cluster.IdentityFile, "orders", "a")
	require.NoError(t, err)
	defer f.Close()
	require.Equal(t, "a", f.State.InstanceID)
}
