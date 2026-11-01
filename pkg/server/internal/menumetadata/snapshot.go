// 本文件从服务冻结注册表生成最小菜单快照，不读取配置或业务数据。
package menumetadata

import (
	"sort"

	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/digitalwayhk/core/pkg/server/locale"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// Path 是仅由内部传输识别的菜单快照操作，不注册HTTP路由。
const Path = "/internal/core/menu-snapshot"

// New 仅投影目标服务自己的Manage路由与服务报表元数据。
func New(service *types.Service, routers []*types.RouterInfo) *smodels.MenuServiceSnapshot {
	snapshot := &smodels.MenuServiceSnapshot{
		Routers: []smodels.MenuRouterSnapshot{},
		Reports: []stats.ReportMenuItem{},
	}
	if service == nil {
		return snapshot
	}
	snapshot.Name = service.Name
	snapshot.Title = locale.Title(service.Instance, locale.ZhCN)
	if snapshot.Title == "" {
		snapshot.Title = snapshot.Name
	}
	snapshot.TitleEN = locale.Title(service.Instance, locale.EnUS)
	for _, info := range routers {
		if info == nil || info.GetPath() == "" {
			continue
		}
		instance := info.GetInstance()
		if hook, ok := instance.(types.IPackRouterHook); ok && hook.GetInstance() != nil {
			instance = hook.GetInstance()
		}
		title := locale.Title(instance, locale.ZhCN)
		if title == "" {
			title = info.GetInstanceName()
		}
		snapshot.Routers = append(snapshot.Routers, smodels.MenuRouterSnapshot{
			Path:         info.GetPath(),
			InstanceName: info.GetInstanceName(),
			Command:      info.GetCommand(),
			Title:        title,
			TitleEN:      locale.Title(instance, locale.EnUS),
		})
	}
	sort.Slice(snapshot.Routers, func(i, j int) bool {
		return snapshot.Routers[i].Path < snapshot.Routers[j].Path
	})
	snapshot.Reports = stats.ListReportMenus(snapshot.Name)
	return snapshot
}
