// 本文件验证 getmenu 按当前语言挑选展示标题，且稳定键不随语言变化。
package public

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/locale"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/stretchr/testify/require"
)

func menuFixture() []*smodels.DirectoryModel {
	dir := smodels.NewDirectoryModel()
	dir.Name = "demo"
	dir.Title = "演示服务"
	dir.TitleEN = "Demo"

	translated := smodels.NewMenuModel()
	translated.Name = "TokenManage"
	translated.Title = "代币管理"
	translated.TitleEN = "Tokens"
	translated.Url = "/api/manage/demo/tokenmanage"

	untranslated := smodels.NewMenuModel()
	untranslated.Name = "OrderManage"
	untranslated.Title = "订单管理"
	untranslated.Url = "/api/manage/demo/ordermanage"

	dir.MenuItems = []*smodels.MenuModel{translated, untranslated}

	legacy := smodels.NewDirectoryModel()
	legacy.Name = "legacy"
	legacy.Title = "旧服务"

	return []*smodels.DirectoryModel{dir, legacy}
}

// 无 X-Locale 的旧前端必须继续看到落库的中文 Title。
func TestApplyMenuLocaleKeepsStoredTitlesForChinese(t *testing.T) {
	dirs := menuFixture()
	applyMenuLocale(dirs, locale.ZhCN)

	require.Equal(t, "演示服务", dirs[0].Title)
	require.Equal(t, "代币管理", dirs[0].MenuItems[0].Title)
	require.Equal(t, "订单管理", dirs[0].MenuItems[1].Title)
	require.Equal(t, "旧服务", dirs[1].Title)
}

func TestApplyMenuLocaleUsesEnglishTitlesWhenPresent(t *testing.T) {
	dirs := menuFixture()
	applyMenuLocale(dirs, locale.EnUS)

	require.Equal(t, "Demo", dirs[0].Title)
	require.Equal(t, "Tokens", dirs[0].MenuItems[0].Title)
}

// 没有英文文案的行回退到中文，而不是变成空白。
func TestApplyMenuLocaleFallsBackToChineseWhenEnglishIsMissing(t *testing.T) {
	dirs := menuFixture()
	applyMenuLocale(dirs, locale.EnUS)

	require.Equal(t, "订单管理", dirs[0].MenuItems[1].Title)
	require.Equal(t, "旧服务", dirs[1].Title)
}

// Name 和 Url 是稳定键，两种语言下必须完全一致。
func TestApplyMenuLocaleLeavesStableKeysUntouched(t *testing.T) {
	chinese := menuFixture()
	applyMenuLocale(chinese, locale.ZhCN)
	english := menuFixture()
	applyMenuLocale(english, locale.EnUS)

	require.Equal(t, chinese[0].Name, english[0].Name)
	for index, item := range chinese[0].MenuItems {
		require.Equal(t, item.Name, english[0].MenuItems[index].Name)
		require.Equal(t, item.Url, english[0].MenuItems[index].Url)
	}
}

func TestApplyMenuLocaleToleratesNilEntries(t *testing.T) {
	dir := smodels.NewDirectoryModel()
	dir.MenuItems = []*smodels.MenuModel{nil}
	require.NotPanics(t, func() {
		applyMenuLocale([]*smodels.DirectoryModel{nil, dir}, locale.EnUS)
	})
}

func TestGetLocalMenuCarriesEnglishTitles(t *testing.T) {
	dir, err := getLocalMenu()
	require.NoError(t, err)
	require.Equal(t, "内部系统管理", dir.Title)
	require.Equal(t, "System", dir.TitleEN)

	expected := map[string][2]string{
		"directorymanage": {"目录管理", "Directories"},
		"menumanage":      {"菜单管理", "Menus"},
		"configsettings":  {"配置设置", "Settings"},
		"aiprovider":      {"AI 提供商", "AI Provider"},
	}
	require.Len(t, dir.MenuItems, len(expected))
	for _, item := range dir.MenuItems {
		titles, ok := expected[item.Name]
		require.True(t, ok, "未预期的内置菜单 %s", item.Name)
		require.Equal(t, titles[0], item.Title)
		require.Equal(t, titles[1], item.TitleEN)
	}
}

// 内置系统菜单在 en-US 下必须整体显示英文。
func TestGetLocalMenuRendersEnglishUnderEnUS(t *testing.T) {
	dir, err := getLocalMenu()
	require.NoError(t, err)
	urls := make(map[string]string, len(dir.MenuItems))
	for _, item := range dir.MenuItems {
		urls[item.Name] = item.Url
	}

	applyMenuLocale([]*smodels.DirectoryModel{dir}, locale.EnUS)

	require.Equal(t, "System", dir.Title)
	require.Equal(t, "server", dir.Name)
	for _, item := range dir.MenuItems {
		require.NotEmpty(t, item.TitleEN)
		require.Equal(t, item.TitleEN, item.Title)
		require.Equal(t, urls[item.Name], item.Url)
	}
}
