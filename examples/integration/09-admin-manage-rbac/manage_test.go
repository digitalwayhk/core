package adminrbac_test

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestDemoBusinessManage 启动 09 的真实入口，验证页面 schema、持久化和只读角色拒绝写入。
func TestDemoBusinessManage(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	viewPort := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	suite, err := integration.StartProcess(integration.ProcessOptions{
		BuildPackage: "./examples/09-admin-manage-rbac/main",
		BinaryName:   "admin-manage-rbac", TempPrefix: "core-admin-rbac-",
		ServiceCount: 2, ServiceIndex: 1, GRPCServiceCount: 2,
		Arguments: []string{"-view", fmt.Sprint(viewPort)},
	})
	require.NoError(t, err, "09 必须注册可管理的业务服务")
	t.Cleanup(suite.Stop)
	t.Cleanup(func() {
		if t.Failed() {
			suite.PrintLog()
		}
	})
	admin := suite.TokenFor(t, "demo-admin", 1)
	// 通用进程 helper 等待业务端口；开发视图 listener 另行异步启动，需要单独等待。
	require.Eventually(t, func() bool {
		connection, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", viewPort), 100*time.Millisecond)
		if err != nil {
			return false
		}
		_ = connection.Close()
		return true
	}, 10*time.Second, 25*time.Millisecond, "开发视图未开始监听")
	system := &integration.Suite{BaseURL: fmt.Sprintf("http://127.0.0.1:%d", viewPort)}
	systemToken := system.TokenFor(t, "menu-admin", 1)
	updated := system.RequestJSON(t, http.MethodPost, "/api/manage/server/menumanage/updatemenu", systemToken, nil)
	require.True(t, updated.Success, updated.Body)
	menus := system.RequestJSON(t, http.MethodGet, "/api/servermanage/getmenu", "", nil)
	require.True(t, menus.Success, menus.Body)
	require.Contains(t, string(menus.Data), "分类管理")
	require.Contains(t, string(menus.Data), "资料条目")
	require.Contains(t, string(menus.Data), "/api/manage/catalog/categorymanage/add")
	require.Contains(t, string(menus.Data), "/api/manage/server/managerolepermissionmanage/savepermissionselection")
	for _, entry := range []struct{ name, title string }{
		{"managerolemanage", "角色管理"},
		{"managerolepermissionmanage", "角色权限管理"},
		{"manageprincipalmanage", "管理员"},
		{"manageprincipalrolemanage", "管理员角色绑定"},
	} {
		path := "/api/manage/server/" + entry.name
		require.Contains(t, string(menus.Data), path)
		require.Contains(t, string(menus.Data), entry.title)
		for _, command := range []string{"view", "search"} {
			response := system.RequestJSON(t, http.MethodPost, path+"/"+command, systemToken, map[string]int{"page": 1, "size": 50})
			require.True(t, response.Success, response.Body)
			if entry.name == "managerolepermissionmanage" && command == "view" {
				require.NotContains(t, string(response.Data), `"command":"permissionselection"`)
				require.NotContains(t, string(response.Data), `"command":"savepermissionselection"`)
				require.Contains(t, string(response.Data), `"command":"bindmenu"`)
			}
		}
	}
	for _, page := range []string{"categorymanage", "catalogitemmanage"} {
		response := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/"+page+"/view", admin, nil)
		require.True(t, response.Success, response.Body)
		require.Contains(t, string(response.Data), `"command":"add"`)
		require.Contains(t, string(response.Data), `"command":"edit"`)
		if page == "catalogitemmanage" {
			require.Contains(t, string(response.Data), `"command":"cloneitem"`)
		}
	}

	// 仅在隔离测试中读取临时配置签发角色令牌；正式身份由 Casdoor callback/refresh 签发。
	data, err := os.ReadFile(filepath.Join(suite.RootDir, "etc", "catalog.json"))
	require.NoError(t, err)
	var cfg struct {
		ManageAuth struct{ AccessSecret string }
	}
	require.NoError(t, json.Unmarshal(data, &cfg))
	claims := safe.NewClaims("demo-viewer", "只读演示用户")
	require.NoError(t, claims.SetManageRoles([]types.ManageRoleRef{{Code: types.ManageRoleViewer}}))
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: claims, Identity: types.AuthIdentity{UID: "demo-viewer", AuthType: types.AuthTypeManage},
		AuthType: types.AuthTypeManage, IssuedAt: time.Now().UTC().Add(-time.Second),
		AccessSecret: cfg.ManageAuth.AccessSecret, AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	path := "/api/manage/catalog/categorymanage/"
	for _, command := range []string{"view", "search"} {
		response := suite.RequestJSON(t, http.MethodPost, path+command, pair.AccessToken, map[string]int{"page": 1, "size": 50})
		require.True(t, response.Success, response.Body)
	}
	body := map[string]interface{}{"code": "rbac-demo", "name": "权限演示分类", "kind": 0, "enabled": true}
	denied := suite.RequestJSON(t, http.MethodPost, path+"add", pair.AccessToken, body)
	require.Equal(t, http.StatusForbidden, denied.HTTPStatus)
	require.JSONEq(t, `{"success":false,"code":40300,"message":"permission denied"}`, denied.Body)
	created := suite.RequestJSON(t, http.MethodPost, path+"add", admin, body)
	require.True(t, created.Success, created.Body)
	search := suite.RequestJSON(t, http.MethodPost, path+"search", pair.AccessToken, map[string]int{"page": 1, "size": 50})
	require.True(t, search.Success, search.Body)
	require.Contains(t, string(search.Data), "权限演示分类")
	roleCode := "demo.tree_editor"
	role := system.RequestJSON(t, http.MethodPost, "/api/manage/server/managerolemanage/add", systemToken,
		map[string]interface{}{"code": roleCode, "name": "树选择演示角色", "enabled": true})
	require.True(t, role.Success, role.Body)
	customClaims := safe.NewClaims("demo-editor", "自定义角色用户")
	require.NoError(t, customClaims.SetManageRoles([]types.ManageRoleRef{{Code: roleCode}}))
	customPair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: customClaims, Identity: types.AuthIdentity{UID: "demo-editor", AuthType: types.AuthTypeManage},
		AuthType: types.AuthTypeManage, IssuedAt: time.Now().UTC().Add(-time.Second),
		AccessSecret: cfg.ManageAuth.AccessSecret, AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	selectionPath := "/api/manage/server/managerolepermissionmanage/"
	permissions := []map[string]string{
		{"service": "catalog", "path": path + "view", "command": "view"},
		{"service": "catalog", "path": path + "add", "command": "add"},
	}
	for _, selected := range [][]map[string]string{permissions, permissions[:1], {}} {
		saved := system.RequestJSON(t, http.MethodPost, selectionPath+"savepermissionselection", systemToken,
			map[string]interface{}{"roleCode": roleCode, "permissions": selected})
		require.True(t, saved.Success, saved.Body)
		loaded := system.RequestJSON(t, http.MethodPost, selectionPath+"permissionselection", systemToken, map[string]string{"roleCode": roleCode})
		require.True(t, loaded.Success, loaded.Body)
		var rows []map[string]interface{}
		require.NoError(t, json.Unmarshal(loaded.Data, &rows))
		require.Len(t, rows, len(selected))
		// 同一 Token 随保存结果立即获得或失去权限，证明不是只改界面显示。
		for _, command := range []string{"view", "add"} {
			response := suite.RequestJSON(t, http.MethodPost, path+command, customPair.AccessToken,
				map[string]interface{}{"code": "tree-grant", "name": "树授权分类", "enabled": true})
			allowed := len(selected) > 0 && (command == "view" || len(selected) == 2)
			if allowed {
				require.True(t, response.Success, response.Body)
			} else {
				require.Equal(t, http.StatusForbidden, response.HTTPStatus, response.Body)
			}
		}
	}
	for _, bad := range []map[string]interface{}{
		{"roleCode": "core.system_admin", "permissions": permissions},
		{"roleCode": roleCode, "permissions": []map[string]string{{"service": "catalog", "path": path + "fake", "command": "fake"}}},
		{"roleCode": roleCode},
	} {
		response := system.RequestJSON(t, http.MethodPost, selectionPath+"savepermissionselection", systemToken, bad)
		require.False(t, response.Success, response.Body)
	}
	unauthorized := system.RequestJSON(t, http.MethodPost, selectionPath+"savepermissionselection", pair.AccessToken,
		map[string]interface{}{"roleCode": roleCode, "permissions": permissions})
	require.Equal(t, http.StatusForbidden, unauthorized.HTTPStatus)
	for _, name := range []string{"server.json", "catalog.json"} {
		data, err := os.ReadFile(filepath.Join(suite.RootDir, "etc", name))
		require.NoError(t, err)
		var generated struct{ MQ struct{ Mode string } }
		require.NoError(t, json.Unmarshal(data, &generated))
		require.Equal(t, "off", generated.MQ.Mode, name)
	}
	logData, err := os.ReadFile(filepath.Join(suite.RootDir, "service.log"))
	require.NoError(t, err)
	require.NotContains(t, string(logData), "mq_degraded")
	require.NotContains(t, string(logData), "redis: connection pool")
}
