package smodels

import (
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/internal/managestore"
	"github.com/stretchr/testify/require"
)

// TestNewManageModelListUsesConfiguredStore 验证系统 Manage 仅通过 models 无参数工厂取得共享控制面存储。
func TestNewManageModelListUsesConfiguredStore(t *testing.T) {
	resetManageStoreForTest(t)
	database := filepath.Join(t.TempDir(), "configured_control_plane")
	action := oltp.NewFixedSqlite(database)
	require.NoError(t, managestore.Configure(action))

	list := NewManageModelList[ManageRoleModel]()
	item := list.GetSearchItem()
	item.Model = NewManageRoleModel()

	require.Contains(t, syncPoolKey(t, list.GetDBAdapter(item), item.Model), database)
}

// TestSystemManageModelsShareConfiguredStore 验证七类系统模型使用同一个控制面数据源。
func TestSystemManageModelsShareConfiguredStore(t *testing.T) {
	resetManageStoreForTest(t)
	database := filepath.Join(t.TempDir(), "shared_control_plane")
	require.NoError(t, managestore.Configure(oltp.NewFixedSqlite(database)))

	require.Contains(t, modelListDBKey(t, NewManageModelList[DirectoryModel](), *NewDirectoryModel()), database)
	require.Contains(t, modelListDBKey(t, NewManageModelList[MenuModel](), *NewMenuModel()), database)
	require.Contains(t, modelListDBKey(t, NewManageModelList[PermissionsModel](), *NewPermissionsModel()), database)
	require.Contains(t, modelListDBKey(t, NewManageModelList[ManageRoleModel](), *NewManageRoleModel()), database)
	require.Contains(t, modelListDBKey(t, NewManageModelList[ManageRolePermissionModel](), *NewManageRolePermissionModel()), database)
	require.Contains(t, modelListDBKey(t, NewManageModelList[ManagePrincipalModel](), *NewManagePrincipalModel()), database)
	require.Contains(t, modelListDBKey(t, NewManageModelList[ManagePrincipalRoleModel](), *NewManagePrincipalRoleModel()), database)
}

// TestNewManageModelListClonesMutableAction 验证各请求共享连接池但不共享事务状态。
func TestNewManageModelListClonesMutableAction(t *testing.T) {
	resetManageStoreForTest(t)
	require.NoError(t, managestore.Configure(oltp.NewFixedSqlite(filepath.Join(t.TempDir(), "cloned_control_plane"))))

	first := NewManageModelList[ManageRoleModel]().GetAction()
	second := NewManageModelList[ManageRoleModel]().GetAction()

	require.NotSame(t, first, second)
}

func resetManageStoreForTest(t *testing.T) {
	t.Helper()
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
}

func modelListDBKey[T persistencetype.IModel](t *testing.T, list *entity.ModelList[T], model T) string {
	t.Helper()
	item := list.GetSearchItem()
	item.Model = model
	return syncPoolKey(t, list.GetDBAdapter(item), item.Model)
}

func syncPoolKey(t *testing.T, action persistencetype.IDataAction, model interface{}) string {
	t.Helper()
	keyed, ok := action.(interface{ GetSyncPoolKey(interface{}) string })
	require.True(t, ok)
	return keyed.GetSyncPoolKey(model)
}
