// 本文件通过正式 ServiceContext 验证副本身份与编号跨正常重启稳定。
package router_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/stretchr/testify/require"
)

// TestStableContextRestart 验证重新构造后的实例 ID、编号、生成时间都保持契约。
func TestStableContextRestart(t *testing.T) {
	name := uniqueServiceName("stable-restart")
	root := t.TempDir()
	file := filepath.Join(root, "instance", "identity.json")
	require.NoError(t, identity.Create(file, name, "instance-a"))
	c := testServiceConfig(name, 31990)
	c.Cluster.IdentityMode = "stable"
	c.Cluster.IdentityFile = file
	c.Cluster.InstanceID = "instance-a"
	c.DataCenterID = 1
	c.MachineID = 0
	c.IDGenerator = &config.IDGeneratorConfig{Provider: "local", Namespace: "test", Epoch: "epoch-a", LocalDevelopment: true, LocalPath: filepath.Join(root, "authority.json"), ReservationWindow: time.Millisecond, StartupTimeout: time.Second}
	a, _, err := identity.BuildAuthority(c.IDGenerator)
	require.NoError(t, err)
	require.NoError(t, a.Initialize(context.Background()))
	require.NoError(t, a.Close())
	service := &instrumentedService{name: name}
	first := router.NewServiceContextWithConfig(service, c)
	first.SetRunState(true)
	firstID := first.NewID()
	firstInstance := first.ServiceInstanceID
	machine := first.Config.MachineID
	first.SetRunState(false)
	second := router.NewServiceContextWithConfig(service, c)
	second.SetRunState(true)
	defer second.SetRunState(false)
	require.Equal(t, firstInstance, second.ServiceInstanceID)
	require.Equal(t, machine, second.Config.MachineID)
	require.Greater(t, second.NewID(), firstID)
}

// TestMetadataRequestCannotAllocateBusinessID 验证无服务上下文的元数据请求不能绕过编号权威。
func TestMetadataRequestCannotAllocateBusinessID(t *testing.T) {
	require.Panics(t, func() { (&router.InitRequest{}).NewID() })
}
