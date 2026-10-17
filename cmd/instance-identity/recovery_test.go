// 本文件串联可信历史导入、崩溃恢复、退役预览与正式应用。
package main

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineImportRecoveryAndRetirement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	c := config.NewServiceDefaultConfig("orders", 18080)
	c.Cluster.Mode = "off"
	c.Cluster.IdentityMode = "stable"
	c.Cluster.IdentityFile = filepath.Join(stateDir, "identity.json")
	c.IDGenerator = &config.IDGeneratorConfig{Provider: "local", Namespace: "test", Epoch: "epoch-a", LocalPath: filepath.Join(root, "authority.json"), LocalDevelopment: true}
	write := func(path string, value interface{}) {
		t.Helper()
		data, err := json.Marshal(value)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0600))
	}
	write(path, c)
	require.NoError(t, run([]string{"init-authority", "--config", path}))
	m := importManifest{State: identity.State{Version: 1, Service: "orders", InstanceID: "a", GeneratorState: identity.GeneratorState{Assigned: true, Namespace: "test", AuthorityEpoch: "epoch-a", Encoding: "legacy-decimal-v1", DataCenterID: 1, MachineID: 0, WorkerID: 10}}, StoppedEvidence: "旧进程已隔离", PendingPath: filepath.Join(root, "original-pending")}
	manifest := filepath.Join(root, "import.json")
	write(manifest, m)
	require.NoError(t, run([]string{"import", "--config", path, "--manifest", manifest}))
	require.Error(t, run([]string{"import", "--config", path, "--manifest", manifest}), "不能覆盖身份文件")
	a, _, err := identity.BuildAuthority(c.IDGenerator)
	require.NoError(t, err)
	defer a.Close()
	ctx := context.Background()
	key := identity.InstanceKey("orders", "a")
	epoch, err := a.AcquireBoot(ctx, key, "boot-a", "", false)
	require.NoError(t, err)
	f, err := identity.Open(c.Cluster.IdentityFile, "orders", "a")
	require.NoError(t, err)
	f.State.BootID = "boot-a"
	f.State.BootEpoch = epoch
	require.NoError(t, f.Save())
	proof := identity.RecoveryProof{Key: key, Epoch: "epoch-a", BootID: "boot-a", BootEpoch: epoch, StoppedEvidence: "测试旧进程已退出且无法重启", PendingEvidence: "原目录及 Outbox 已核对保留"}
	recoverFile := filepath.Join(root, "recover.json")
	write(recoverFile, proof)
	require.Error(t, run([]string{"recover", "--config", path, "--manifest", recoverFile, "--apply"}), "持锁进程仍存在，禁止批准")
	require.NoError(t, f.Close())
	require.NoError(t, run([]string{"recover", "--config", path, "--manifest", recoverFile, "--dry-run"}))
	require.NoError(t, a.VerifyBoot(ctx, key, "boot-a", epoch))
	require.NoError(t, run([]string{"recover", "--config", path, "--manifest", recoverFile, "--apply"}))
	require.Error(t, a.VerifyBoot(ctx, key, "boot-a", epoch))
	retired := identity.RetirementProof{Key: key, Epoch: "epoch-a", BootEpoch: epoch, StoppedEvidence: "已停止且卷封存", PendingEvidence: "已核对"}
	retireFile := filepath.Join(root, "retire.json")
	write(retireFile, retired)
	require.NoError(t, run([]string{"retire", "--config", path, "--manifest", retireFile, "--dry-run"}))
	b, err := a.Get(ctx, key)
	require.NoError(t, err)
	require.False(t, b.Retired)
	require.NoError(t, run([]string{"retire", "--config", path, "--manifest", retireFile, "--apply"}))
	b, err = a.Get(ctx, key)
	require.NoError(t, err)
	require.True(t, b.Retired)
	_, err = a.AcquireBoot(ctx, key, "boot-old", "boot-a", false)
	require.Error(t, err)
}
