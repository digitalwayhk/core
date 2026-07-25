package manage

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/zeromicro/go-zero/core/logx"
)

type MenuManage struct {
	*DmpBase[smodels.MenuModel]
}

func NewMenuManage() *MenuManage {
	own := &MenuManage{}
	own.DmpBase = NewDmpBase[smodels.MenuModel](own)
	return own
}
func (own *MenuManage) Routers() []types.IRouter {
	routers := own.DmpBase.Routers()
	routers = append(routers, own.Remove)
	routers = append(routers, NewUpdateMenu(own))
	return routers
}
func (own *MenuManage) ViewCommandModel(cmd *view.CommandModel) {
	if cmd.Name == "UpdateMenu" {
		cmd.Title = "更新菜单"
		cmd.IsSelectRow = false
	}
}
func (own *MenuManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.DmpBase.ViewFieldModel(model, field)
	if field.IsFieldOrTitle("directorymodelid") {
		field.IsEdit = true
		field.Visible = true
		field.Title = "目录"
		if field.Foreign != nil {
			field.Foreign.OneDisplayName = "title"
			field.Foreign.FModel.Fields = append(field.Foreign.FModel.Fields, &view.FieldModel{
				Field:    "title",
				Title:    "标题",
				Visible:  true,
				IsSearch: true,
			})
		}
	}
}
func (own *MenuManage) updateMenuModelAll(req types.IRequest) error {
	if own == nil || own.DmpBase == nil {
		return errors.New("MenuManage list unavailable")
	}
	list, ok := own.GetList().(*entity.ModelList[smodels.MenuModel])
	if !ok || list == nil {
		return errors.New("MenuManage list unavailable")
	}
	search := list.GetSearchItem()
	search.Model = smodels.NewMenuModel()
	action := list.GetDBAdapter(search)
	if action == nil {
		action = list.GetAction()
	}
	return syncMenusAtomic(action, own.GetDefaultItemsWithRequest(req))
}

// GetDefaultItems 保留用于兼容旧业务扩展。
// Deprecated: 已废弃，请使用 GetDefaultItemsWithRequest 显式传入请求。
func (own *MenuManage) GetDefaultItems() []*smodels.MenuModel {
	return own.GetDefaultItemsWithRequest(own.Req)
}

func (own *MenuManage) GetDefaultItemsWithRequest(req types.IRequest) []*smodels.MenuModel {
	items := make([]*smodels.MenuModel, 0)
	dir := NewDirectoryManage()
	dirList := dir.GetList().(*entity.ModelList[smodels.DirectoryModel])
	scs := router.GetContexts()
	for _, sc := range scs {
		if sc.Service.Name == "server" {
			continue // 排除 server 服务
		}
		data := sc.Router.GetTypeRouters(types.ApiType(types.ManageType))
		for _, info := range data {
			path := info.GetPath()
			instanceName := info.GetInstanceName()
			if path == "" {
				continue
			}
			item := getMenuModel(info, items)
			if item == nil {
				item = smodels.NewMenuModel()
				item.Name = instanceName
				name := strings.ToLower(item.Name)
				item.Url = buildMenuUrl(path, name)
				item.Permissions = make([]*smodels.PermissionsModel, 0)
				dirrows, err := dirList.SearchName(sc.Service.Name)
				if err != nil {
					continue
				}
				if len(dirrows) > 0 {
					item.DirectoryModelID = dirrows[0].ID
				} else {
					diritem := own.newDirectoryModel(req, sc)
					if err := dirList.Add(diritem); err != nil {
						logx.Errorf("Add directory model error: %v", err)
						continue
					}
					if err := dirList.Save(); err != nil {
						logx.Errorf("Save directory model error: %v", err)
						continue
					}
					item.DirectoryModelID = diritem.ID
				}
				items = append(items, item)
			}
			cmds := strings.Split(path, "/")
			if len(cmds) > 0 {
				cmd := cmds[len(cmds)-1]
				if cmd != "" {
					npm := smodels.NewPermissionsModel()
					npm.Name = cmd
					npm.Url = path
					item.Permissions = append(item.Permissions, npm)
				}
			}
		}
	}
	return items
}

func (own *MenuManage) newDirectoryModel(req types.IRequest, sc *router.ServiceContext) *smodels.DirectoryModel {
	diritem := smodels.NewDirectoryModel()
	diritem.Name = sc.Service.Name
	diritem.ID = req.NewID()
	if ititle, ok := sc.Service.Instance.(types.ITitle); ok {
		diritem.Title = ititle.GetTitle()
	}
	return diritem
}

// buildMenuUrl 依据 path 中 name 出现的位置拼装菜单分组 Url。
// 当 name 未出现在 path 中时（例如自定义操作的实例名与路径不一致），
// strings.Index 会返回 -1，直接切片会导致 "slice bounds out of range [:-1]" panic，
// 因此这里显式兜底，退化为使用完整 path 作为分组依据。
func buildMenuUrl(path, name string) string {
	endIndex := strings.Index(path, name)
	if endIndex < 0 {
		return path
	}
	return path[0:endIndex] + name
}

func getMenuModel(info *types.RouterInfo, items []*smodels.MenuModel) *smodels.MenuModel {
	instanceName := info.GetInstanceName()
	path := info.GetPath()
	name := strings.ToLower(instanceName)
	url := buildMenuUrl(path, name)
	for _, item := range items {
		if item.Name == instanceName && item.Url == url {
			return item
		}
	}
	return nil
}

type UpdateMenu struct {
	manage.Operation[smodels.MenuModel]
}

func NewUpdateMenu(instance interface{}) *UpdateMenu {
	return &UpdateMenu{
		Operation: manage.NewOperation[smodels.MenuModel](instance),
	}
}
func (own *UpdateMenu) New(instance interface{}) types.IRouter {
	if own.GetInstance() == nil {
		own.Operation.New(instance)
	}
	return own
}
func (own *UpdateMenu) Validation(req types.IRequest) error {
	return nil
}
func (own *UpdateMenu) Do(req types.IRequest) (interface{}, error) {
	if own.GetInstance() == nil {
		return nil, errors.New("UpdateMenu instance is nil")
	}
	mm, ok := own.GetInstance().(*MenuManage)
	if !ok {
		return nil, errors.New("UpdateMenu instance must be MenuManage")
	}
	if err := mm.updateMenuModelAll(req); err != nil {
		return nil, err
	}
	return nil, nil
}
func (own *UpdateMenu) RouterInfo() *types.RouterInfo {
	return manage.RouterInfo(own)
}
