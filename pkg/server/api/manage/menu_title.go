// 本文件从服务实例和 Manage 控制器推导目录、菜单的中英展示标题。
package manage

import (
	"github.com/digitalwayhk/core/pkg/server/locale"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// manageOwner 返回路由背后真正的 Manage 控制器。
// View、Search 等泛型操作对象只是包装，标题声明在它们包装的控制器上。
func manageOwner(info *types.RouterInfo) interface{} {
	if info == nil {
		return nil
	}
	instance := info.GetInstance()
	if hook, ok := instance.(types.IPackRouterHook); ok && hook.GetInstance() != nil {
		return hook.GetInstance()
	}
	return instance
}

// localeTitles 返回落库用的中英标题。
// 只实现 ITitle 或什么都不实现时英文标题为空，由 getmenu 回退中文。
func localeTitles(instance interface{}, fallback string) (title string, titleEN string) {
	title = locale.Title(instance, locale.ZhCN)
	if title == "" {
		title = fallback
	}
	return title, locale.Title(instance, locale.EnUS)
}

// generatedDirectoryTitlesFromSnapshots 从本地或远程服务快照生成目录标题刷新集合。
func generatedDirectoryTitlesFromSnapshots(snapshots []*smodels.MenuServiceSnapshot) []*smodels.DirectoryModel {
	items := make([]*smodels.DirectoryModel, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshot == nil || snapshot.Name == "" || snapshot.Name == "server" {
			continue
		}
		item := smodels.NewDirectoryModel()
		item.Name = snapshot.Name
		item.Title = snapshot.Title
		if item.Title == "" {
			item.Title = item.Name
		}
		item.TitleEN = snapshot.TitleEN
		items = append(items, item)
	}
	return items
}

// directoryTitlesChanged 与菜单同一规则：展示标题独立于其它字段判断是否需要刷新。
func directoryTitlesChanged(old, generated *smodels.DirectoryModel) bool {
	if old == nil || generated == nil {
		return false
	}
	if generated.Title != "" && old.Title != generated.Title {
		return true
	}
	return old.TitleEN != generated.TitleEN
}

// mergeGeneratedDirectory 只覆盖展示标题，Sort、Icon、Description 仍属用户字段。
func mergeGeneratedDirectory(old, generated *smodels.DirectoryModel) *smodels.DirectoryModel {
	if old == nil {
		return generated
	}
	if generated == nil {
		return old
	}
	if generated.Title != "" {
		old.Title = generated.Title
	}
	old.TitleEN = generated.TitleEN
	return old
}
