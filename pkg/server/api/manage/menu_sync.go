// 本文件提供菜单扫描结果的稳定权限比较和用户字段合并规则。
package manage

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/smodels"
)

func permissionKey(permission *smodels.PermissionsModel) string {
	if permission == nil {
		return ""
	}
	return strings.TrimSpace(permission.Name) + "\x00" + strings.TrimSpace(permission.Url)
}

func normalizedPermissionSet(items []*smodels.PermissionsModel) map[string]*smodels.PermissionsModel {
	result := make(map[string]*smodels.PermissionsModel, len(items))
	for _, item := range items {
		if key := permissionKey(item); key != "" {
			result[key] = item
		}
	}
	return result
}

func permissionSetsChanged(oldItems, newItems []*smodels.PermissionsModel) bool {
	oldSet := normalizedPermissionSet(oldItems)
	newSet := normalizedPermissionSet(newItems)
	if len(oldSet) != len(newSet) {
		return true
	}
	for key := range oldSet {
		if _, exists := newSet[key]; !exists {
			return true
		}
	}
	return false
}

// displayTitlesChanged 判断展示标题是否需要刷新。与权限比较分开，
// 使权限未变的存量菜单仍能拿到代码里新写的中英标题。
func displayTitlesChanged(old, generated *smodels.MenuModel) bool {
	if old == nil || generated == nil {
		return false
	}
	if generated.Title != "" && old.Title != generated.Title {
		return true
	}
	return old.TitleEN != generated.TitleEN
}

// mergeGeneratedMenu 保留 Sort、Icon、Description 等用户字段，
// 但 Title 和 TitleEN 以生成结果为准——翻译的权威源是代码而不是数据库行。
// 生成结果的 Title 为空时保留原值，避免把已有标题抹成空白。
func mergeGeneratedMenu(old, generated *smodels.MenuModel) *smodels.MenuModel {
	if old == nil {
		return generated
	}
	if generated == nil {
		return old
	}
	old.Name = generated.Name
	old.Url = generated.Url
	if generated.Title != "" {
		old.Title = generated.Title
	}
	old.TitleEN = generated.TitleEN
	return old
}
