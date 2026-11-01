// 本文件验证菜单只读快照经过真实入站处理器，且不会授予ServerManage权限。
package router

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestInternalMenuSnapshotInboundBoundary 覆盖可信服务、自报身份、错目标及管理路径隔离。
func TestInternalMenuSnapshotInboundBoundary(t *testing.T) {
	const service = "menu-snapshot-inbound"
	route := &localInternalCallerRoute{}
	route.info = &types.RouterInfo{Path: "/api/manage/" + service + "/itemmanage/search", ServiceName: service,
		PathType: types.ManageType, Method: http.MethodPost, InstanceName: "ItemManage", StructName: "ItemManage"}
	route.info.SetInstance(route)
	cfg := config.NewServiceDefaultConfig(service, 0)
	cfg.Cluster.Mode, cfg.MQ.Mode = "off", "off"
	sc := NewServiceContextWithConfig(&localInternalCallerService{name: service, route: route}, cfg)
	t.Cleanup(func() { sc.SetRunState(false) })
	trusted := types.ContextWithTrustedInternalCaller(context.Background(), "menu-authority")
	payload := func() *types.PayLoad {
		return &types.PayLoad{SourceService: "menu-authority", TargetService: service,
			TargetPath: "/internal/core/menu-snapshot", HttpMethod: http.MethodPost, Data: []byte(`{}`)}
	}
	t.Run("trusted-read-only", func(t *testing.T) {
		raw, err := sc.HandleInternalPayload(trusted, payload())
		require.NoError(t, err)
		var response struct {
			Success bool                        `json:"success"`
			Data    smodels.MenuServiceSnapshot `json:"data"`
		}
		require.NoError(t, json.Unmarshal(raw, &response))
		require.True(t, response.Success)
		require.Equal(t, service, response.Data.Name)
		require.Len(t, response.Data.Routers, 1)
		require.Equal(t, route.info.GetPath(), response.Data.Routers[0].Path)
	})
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		source string
	}{
		{"self-reported", context.Background(), "menu-authority"},
		{"forged-source", trusted, "another-service"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := payload()
			p.SourceService = tc.source
			_, err := sc.HandleInternalPayload(tc.ctx, p)
			require.ErrorIs(t, err, types.ErrInternalCallerForbidden)
		})
	}
	t.Run("invalid-operation", func(t *testing.T) {
		for _, mutation := range []func(*types.PayLoad){
			func(p *types.PayLoad) { p.HttpMethod = http.MethodGet },
			func(p *types.PayLoad) { p.Data = make([]byte, 4097) },
		} {
			p := payload()
			mutation(p)
			_, err := sc.HandleInternalPayload(trusted, p)
			require.ErrorIs(t, err, types.ErrInternalCallerForbidden)
		}
	})
	t.Run("wrong-target", func(t *testing.T) {
		p := payload()
		p.TargetService = "other"
		_, err := sc.HandleInternalPayload(trusted, p)
		require.ErrorIs(t, err, ErrTargetServiceUnavailable)
	})
	t.Run("servermanage-still-denied", func(t *testing.T) {
		for _, path := range []string{"/api/servermanage/queryrouters", "/api/servermanage/queryconfig", "/api/servermanage/testtoken"} {
			p := payload()
			p.TargetPath = path
			_, err := sc.HandleInternalPayload(trusted, p)
			require.ErrorIs(t, err, types.ErrInternalCallerForbidden)
		}
	})
}
