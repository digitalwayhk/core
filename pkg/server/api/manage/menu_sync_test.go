// 本文件验证菜单扫描结果的集合比较、用户字段保留和错误传播。
package manage

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/stretchr/testify/require"
)

func TestPermissionSetsChangedDetectsSameLengthReplacement(t *testing.T) {
	old := []*smodels.PermissionsModel{
		{Name: "view", Url: "/api/manage/a/view"},
		{Name: "edit", Url: "/api/manage/a/edit"},
	}
	next := []*smodels.PermissionsModel{
		{Name: "view", Url: "/api/manage/a/view"},
		{Name: "remove", Url: "/api/manage/a/remove"},
	}
	require.True(t, permissionSetsChanged(old, next))
}

func TestPermissionSetsChangedIgnoresOrderAndDuplicates(t *testing.T) {
	old := []*smodels.PermissionsModel{
		{Name: "edit", Url: "/api/manage/a/edit"},
		{Name: "view", Url: "/api/manage/a/view"},
	}
	next := []*smodels.PermissionsModel{
		{Name: "view", Url: "/api/manage/a/view"},
		{Name: "edit", Url: "/api/manage/a/edit"},
		{Name: "view", Url: "/api/manage/a/view"},
	}
	require.False(t, permissionSetsChanged(old, next))
}

func TestMergeGeneratedMenuPreservesUserFields(t *testing.T) {
	old := smodels.NewMenuModel()
	old.ID = 42
	old.DirectoryModelID = 9
	old.Title = "用户标题"
	old.Sort = 7
	old.Icon = "custom"
	old.Description = "用户说明"

	generated := smodels.NewMenuModel()
	generated.Name = "TokenManage"
	generated.Url = "/api/manage/token"

	merged := mergeGeneratedMenu(old, generated)
	require.Same(t, old, merged)
	require.Equal(t, uint(42), merged.ID)
	require.Equal(t, uint(9), merged.DirectoryModelID)
	require.Equal(t, "用户标题", merged.Title)
	require.Equal(t, 7, merged.Sort)
	require.Equal(t, "custom", merged.Icon)
	require.Equal(t, "用户说明", merged.Description)
	require.Equal(t, "TokenManage", merged.Name)
	require.Equal(t, "/api/manage/token", merged.Url)
}

func TestUpdateMenuDoPropagatesSyncError(t *testing.T) {
	op := NewUpdateMenu(&MenuManage{})
	require.NotPanics(t, func() {
		_, err := op.Do(nil)
		require.ErrorContains(t, err, "list unavailable")
	})
}
