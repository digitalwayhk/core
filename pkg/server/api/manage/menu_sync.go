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

func mergeGeneratedMenu(old, generated *smodels.MenuModel) *smodels.MenuModel {
	if old == nil {
		return generated
	}
	if generated == nil {
		return old
	}
	old.Name = generated.Name
	old.Url = generated.Url
	return old
}
