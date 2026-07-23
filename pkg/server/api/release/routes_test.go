// 本文件验证系统路由迁移后只注册新路径，不恢复已删除的兼容入口。
package release

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCasdoorCallbackRouteMigration(t *testing.T) {
	paths := map[string]bool{}
	for _, item := range Routers() {
		paths[item.RouterInfo().GetPath()] = true
	}
	require.True(t, paths["/api/casdoor/callback"])
	require.False(t, paths["/api/callback"])
}

func TestOpenAPIRouteMigration(t *testing.T) {
	paths := map[string]bool{}
	for _, item := range Routers() {
		paths[item.RouterInfo().GetPath()] = true
	}

	require.True(t, paths["/api/internal/openapi"])
	require.False(t, paths["/api/servermanage/openapi"])
}
