package manage

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/stretchr/testify/require"
)

func TestReplaceRolePermissionsAddsRemovesAndKeepsOtherRoles(t *testing.T) {
	action, _ := newMenuSQLiteAction(t)
	var rows []*smodels.ManageRolePermissionModel
	require.NoError(t, action.Load(&pt.SearchItem{Model: smodels.NewManageRolePermissionModel(), Page: 1, Size: 1}, &rows))
	permission := func(role, command string) *smodels.ManageRolePermissionModel {
		row := smodels.NewManageRolePermissionModel()
		row.RoleCode, row.Service, row.Command = role, "catalog", command
		row.Path = "/api/manage/catalog/categorymanage/" + command
		return row
	}
	require.NoError(t, replaceRolePermissions(action, "demo.other", []*smodels.ManageRolePermissionModel{permission("demo.other", "edit")}))
	require.NoError(t, replaceRolePermissions(action, "demo.editor", []*smodels.ManageRolePermissionModel{permission("demo.editor", "view"), permission("demo.editor", "add")}))
	require.NoError(t, replaceRolePermissions(action, "demo.editor", []*smodels.ManageRolePermissionModel{permission("demo.editor", "search")}))
	list := entity.NewModelList[smodels.ManageRolePermissionModel](action)
	rows, err := list.SearchWhere("RoleCode", "demo.editor")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "search", rows[0].Command)
	action.failInsertAt = action.insertCalls + 1
	require.Error(t, replaceRolePermissions(action, "demo.editor", []*smodels.ManageRolePermissionModel{permission("demo.editor", "add")}))
	rows, err = list.SearchWhere("RoleCode", "demo.editor")
	require.NoError(t, err)
	require.Len(t, rows, 1, "失败保存必须回滚取消勾选产生的删除")
	require.Equal(t, "search", rows[0].Command)
	action.failInsertAt = 0
	rows, err = list.SearchWhere("RoleCode", "demo.other")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, replaceRolePermissions(action, "demo.editor", nil))
	rows, err = list.SearchWhere("RoleCode", "demo.editor")
	require.NoError(t, err)
	require.Empty(t, rows)
}
