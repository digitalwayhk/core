package public

import (
	"net/http"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type GetMenu struct {
}

func (own *GetMenu) Parse(req types.IRequest) error {
	return nil
}
func (own *GetMenu) Validation(req types.IRequest) error {
	return nil
}
func (own *GetMenu) Do(req types.IRequest) (interface{}, error) {
	list := entity.NewModelList[smodels.DirectoryModel](nil)
	dirs, _, err := list.SearchAll(1, 1000, func(item *pt.SearchItem) {
		item.AddSortN("Sort", false)
	})
	if err != nil {
		return nil, err
	}
	if len(dirs) > 0 {
		for _, dir := range dirs {
			list := entity.NewModelList[smodels.MenuModel](nil)
			rows, _, err := list.SearchAll(1, 1000, func(item *pt.SearchItem) {
				item.AddWhereN("DirectoryModelID", dir.ID)
				item.AddSortN("Sort", false)
			})
			if err != nil {
				return nil, err
			}
			dir.MenuItems = rows
		}
	}
	if utils.HasLocalIPAddr(req.GetClientIP()) {
		localMenu, err := getLocalMenu()
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, localMenu)
	}
	return dirs, nil
}

func (own *GetMenu) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own,
		router.WithMethod(http.MethodGet),
		withSystemEndpointRateLimit(),
	)
}

func getLocalMenu() (*smodels.DirectoryModel, error) {
	dir := smodels.NewDirectoryModel()
	dir.ID = 9987243
	dir.Name = "server"
	dir.Title = "内部系统管理"
	dir.Description = "系统管理相关的菜单"
	dir.Sort = 0
	dir.MenuItems = make([]*smodels.MenuModel, 0)
	menu1 := smodels.NewMenuModel()
	menu1.ID = 9987244
	menu1.DirectoryModelID = dir.ID
	menu1.Name = "directorymanage"
	menu1.Title = "目录管理"
	menu1.Description = "管理系统中的目录"
	menu1.Sort = 0
	menu1.Url = "/api/manage/server/directorymanage"
	dir.MenuItems = append(dir.MenuItems, menu1)

	menu2 := smodels.NewMenuModel()
	menu2.ID = 9987245
	menu2.DirectoryModelID = dir.ID
	menu2.Name = "menumanage"
	menu2.Title = "菜单管理"
	menu2.Description = "管理系统中的菜单"
	menu2.Sort = 1
	menu2.Url = "/api/manage/server/menumanage"
	dir.MenuItems = append(dir.MenuItems, menu2)

	menu3 := smodels.NewMenuModel()
	menu3.ID = 9987246
	menu3.DirectoryModelID = dir.ID
	menu3.Name = "configsettings"
	menu3.Title = "配置设置"
	menu3.Description = "查看和编辑各服务的系统配置"
	menu3.Sort = 2
	// 配置页是前端专用页面，不是普通 Manage CRUD 页面。
	menu3.Url = "/config/settings"
	dir.MenuItems = append(dir.MenuItems, menu3)

	menu4 := smodels.NewMenuModel()
	menu4.ID = 9987247
	menu4.DirectoryModelID = dir.ID
	menu4.Name = "aiprovider"
	menu4.Title = "AI 提供商"
	menu4.Description = "配置管理端 PageAgent 使用的 LLM 提供商（方案 A 运行时下发）"
	menu4.Sort = 3
	// 前端专用页面，对应 web/admin/src/pages/config/ai-provider
	menu4.Url = "/config/ai-provider"
	dir.MenuItems = append(dir.MenuItems, menu4)

	return dir, nil
}
