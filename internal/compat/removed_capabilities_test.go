// 本文件锁定已经从现行框架移除的认证、配置和服务依赖能力，防止其被意外恢复。
package compat

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/require"
)

func TestRemovedAuthenticationAndServiceAttachStayAbsent(t *testing.T) {
	_, hasLogto := reflect.TypeOf(config.AuthSecret{}).FieldByName("Logto")
	require.False(t, hasLogto)

	_, hasAttachServices := reflect.TypeOf(config.ServerConfig{}).FieldByName("AttachServices")
	require.False(t, hasAttachServices)
}

func TestRemovedLogtoDependenciesStayAbsent(t *testing.T) {
	root := repositoryRoot(t)
	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	require.NoError(t, err)
	require.NotContains(t, string(goMod), "github.com/MicahParks/keyfunc")
	require.NotContains(t, string(goMod), "github.com/golang-jwt/jwt/v5")

	_, err = os.Stat(filepath.Join(root, "pkg/server/safe/logto"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestRemovedServiceAttachSourcesStayAbsent(t *testing.T) {
	root := repositoryRoot(t)
	sources := map[string][]string{
		"pkg/server/types/service.go": {
			"type ServiceAttach struct",
			"type IAttachService interface",
			"SubscribeRouters []*ObserveArgs",
			"AttachService map[string]*ServiceAttach",
		},
		"pkg/server/types/server.go": {
			"SendNotify(args *NotifyArgs)",
			"SubscribeRouters() []*ObserveArgs",
		},
	}
	for name, removed := range sources {
		contents, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		for _, fragment := range removed {
			require.NotContains(t, string(contents), fragment, name)
		}
	}
}
