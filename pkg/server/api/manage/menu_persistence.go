// 本文件在同一 IDataAction 事务中同步全部菜单及其权限。
package manage

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/utils"
)

func syncMenusAtomic(action persistencetypes.IDataAction, generated []*smodels.MenuModel) (err error) {
	if action == nil {
		return errors.New("menu persistence adapter unavailable")
	}
	if err := prepareMenuPersistence(action); err != nil {
		return err
	}
	if err := action.Transaction(); err != nil {
		return fmt.Errorf("begin menu sync transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := action.Rollback(); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback menu sync transaction: %w", rollbackErr))
		}
	}()

	for _, item := range generated {
		if item == nil {
			continue
		}
		if err := syncOneMenu(action, item); err != nil {
			return err
		}
	}
	if err := action.Commit(); err != nil {
		return fmt.Errorf("commit menu sync transaction: %w", err)
	}
	committed = true
	return nil
}

func prepareMenuPersistence(action persistencetypes.IDataAction) error {
	menuSearch := &persistencetypes.SearchItem{
		Model: smodels.NewMenuModel(),
		Page:  1,
		Size:  1,
	}
	var menus []*smodels.MenuModel
	if err := action.Load(menuSearch, &menus); err != nil {
		return fmt.Errorf("prepare menu persistence: %w", err)
	}

	permissionSearch := &persistencetypes.SearchItem{
		Model: smodels.NewPermissionsModel(),
		Page:  1,
		Size:  1,
	}
	var permissions []*smodels.PermissionsModel
	if err := action.Load(permissionSearch, &permissions); err != nil {
		return fmt.Errorf("prepare permission persistence: %w", err)
	}
	return nil
}

func syncOneMenu(action persistencetypes.IDataAction, generated *smodels.MenuModel) error {
	menuList := entity.NewModelList[smodels.MenuModel](action)
	existing, err := menuList.SearchOne(func(where *persistencetypes.SearchItem) {
		where.AddWhereN("Name", generated.Name)
		where.AddWhereN("Url", generated.Url)
	})
	if err != nil {
		return fmt.Errorf("search menu %s %s: %w", generated.Name, generated.Url, err)
	}
	if existing == nil {
		return insertGeneratedMenu(action, generated)
	}

	permissionList := entity.NewModelList[smodels.PermissionsModel](action)
	oldPermissions, err := permissionList.SearchWhere("MenuModelID", existing.ID)
	if err != nil {
		return fmt.Errorf("load permissions for menu %s: %w", existing.Name, err)
	}
	if !permissionSetsChanged(oldPermissions, generated.Permissions) {
		return nil
	}

	merged := mergeGeneratedMenu(existing, generated)
	merged.Permissions = nil
	if err := action.Update(merged); err != nil {
		return fmt.Errorf("update menu %s: %w", merged.Name, err)
	}
	for _, permission := range oldPermissions {
		if permission == nil || permission.ID == 0 {
			continue
		}
		if err := action.Delete(permission); err != nil {
			return fmt.Errorf("delete permission %s for menu %s: %w", permission.Name, merged.Name, err)
		}
	}
	for _, permission := range preparePermissions(merged.ID, generated.Permissions) {
		if err := action.Insert(permission); err != nil {
			return fmt.Errorf("insert permission %s for menu %s: %w", permission.Name, merged.Name, err)
		}
	}
	return nil
}

func insertGeneratedMenu(action persistencetypes.IDataAction, generated *smodels.MenuModel) error {
	menu := cloneGeneratedMenu(generated)
	menu.Permissions = nil
	menu.SetHashcode(menu.GetHash())
	if err := action.Insert(menu); err != nil {
		return fmt.Errorf("insert menu %s: %w", menu.Name, err)
	}
	for _, permission := range preparePermissions(menu.ID, generated.Permissions) {
		if err := action.Insert(permission); err != nil {
			return fmt.Errorf("insert permission %s for menu %s: %w", permission.Name, menu.Name, err)
		}
	}
	return nil
}

func cloneGeneratedMenu(source *smodels.MenuModel) *smodels.MenuModel {
	menu := smodels.NewMenuModel()
	if source == nil {
		return menu
	}
	menu.Name = source.Name
	menu.Title = source.Title
	menu.Description = source.Description
	menu.Sort = source.Sort
	menu.Icon = source.Icon
	menu.Url = source.Url
	menu.DirectoryModelID = source.DirectoryModelID
	return menu
}

func preparePermissions(menuID uint, items []*smodels.PermissionsModel) []*smodels.PermissionsModel {
	set := normalizedPermissionSet(items)
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	result := make([]*smodels.PermissionsModel, 0, len(keys))
	for _, key := range keys {
		source := set[key]
		item := smodels.NewPermissionsModel()
		item.MenuModelID = menuID
		item.Name = source.Name
		item.Title = source.Title
		item.Description = source.Description
		item.Sort = source.Sort
		item.Icon = source.Icon
		item.Url = source.Url
		item.SetHashcode(utils.HashCodes(fmt.Sprintf(
			"%d|%s|%s",
			menuID,
			strings.TrimSpace(item.Name),
			strings.TrimSpace(item.Url),
		)))
		result = append(result, item)
	}
	return result
}
