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

// 翻译的权威源是代码，同步必须把生成的中英标题写回已有行。
func TestMergeGeneratedMenuOverridesTitlesFromGeneratedResult(t *testing.T) {
	old := smodels.NewMenuModel()
	old.Title = "旧标题"
	old.TitleEN = "Stale"
	old.Sort = 7
	old.Icon = "custom"

	generated := smodels.NewMenuModel()
	generated.Name = "TokenManage"
	generated.Url = "/api/manage/token"
	generated.Title = "代币管理"
	generated.TitleEN = "Tokens"

	merged := mergeGeneratedMenu(old, generated)
	require.Equal(t, "代币管理", merged.Title)
	require.Equal(t, "Tokens", merged.TitleEN)
	require.Equal(t, 7, merged.Sort)
	require.Equal(t, "custom", merged.Icon)
}

// 控制器撤回英文标题时英文列必须清空，不能留下陈旧翻译。
func TestMergeGeneratedMenuClearsStaleEnglishTitle(t *testing.T) {
	old := smodels.NewMenuModel()
	old.Title = "代币管理"
	old.TitleEN = "Stale"

	generated := smodels.NewMenuModel()
	generated.Title = "代币管理"

	merged := mergeGeneratedMenu(old, generated)
	require.Equal(t, "代币管理", merged.Title)
	require.Equal(t, "", merged.TitleEN)
}

func TestDisplayTitlesChanged(t *testing.T) {
	withTitles := func(title, titleEN string) *smodels.MenuModel {
		menu := smodels.NewMenuModel()
		menu.Title = title
		menu.TitleEN = titleEN
		return menu
	}

	require.False(t, displayTitlesChanged(withTitles("代币管理", "Tokens"), withTitles("代币管理", "Tokens")))
	require.True(t, displayTitlesChanged(withTitles("代币管理", ""), withTitles("代币管理", "Tokens")))
	require.True(t, displayTitlesChanged(withTitles("TokenManage", ""), withTitles("代币管理", "")))
	require.True(t, displayTitlesChanged(withTitles("代币管理", "Stale"), withTitles("代币管理", "")))
	// 生成结果没有中文标题时不算变化，避免把已有标题抹成空白
	require.False(t, displayTitlesChanged(withTitles("用户标题", ""), withTitles("", "")))
	require.False(t, displayTitlesChanged(nil, withTitles("代币管理", "Tokens")))
	require.False(t, displayTitlesChanged(withTitles("代币管理", "Tokens"), nil))
}

func TestUpdateMenuDoPropagatesSyncError(t *testing.T) {
	op := NewUpdateMenu(&MenuManage{})
	require.NotPanics(t, func() {
		_, err := op.Do(nil)
		require.ErrorContains(t, err, "list unavailable")
	})
}
