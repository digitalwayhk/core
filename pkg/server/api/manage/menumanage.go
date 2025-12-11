package manage

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
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
func (own *MenuManage) updateMenuModelAll() {
	items := own.GetDefaultItems()
	list := own.GetList().(*entity.ModelList[smodels.MenuModel])
	for _, item := range items {
		old, err := list.SearchOne(func(where *pt.SearchItem) {
			where.AddWhereN("Name", item.Name)
			where.AddWhereN("Url", item.Url)
			where.IsPreload = true
		})
		if err != nil {
			continue
		}
		if old != nil {
			if len(old.Permissions) != len(item.Permissions) {
				item.DirectoryModelID = old.DirectoryModelID
				if err := list.Remove(old); err != nil {
					logx.Errorf("Remove old menu item error: %v", err)
					continue
				}
			} else {
				continue // 如果权限数量相同，则不更新
			}
		}
		if err := list.Add(item); err != nil {
			logx.Errorf("Add menu item error: %v", err)
			continue
		}
		if err := list.Save(); err != nil {
			logx.Errorf("Save menu item error: %v", err)
			continue
		}
	}
}
func (own *MenuManage) GetDefaultItems() []*smodels.MenuModel {
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
			if info.Path == "" {
				continue
			}
			item := getMenuModel(info, items)
			if item == nil {
				item = smodels.NewMenuModel()
				item.Name = info.InstanceName
				name := strings.ToLower(item.Name)
				endIndex := strings.Index(info.Path, name)
				item.Url = info.Path[0:endIndex] + name
				item.Permissions = make([]*smodels.PermissionsModel, 0)
				dirrows, err := dirList.SearchName(sc.Service.Name)
				if err != nil {
					continue
				}
				if len(dirrows) > 0 {
					item.DirectoryModelID = dirrows[0].ID
				} else {
					diritem := smodels.NewDirectoryModel()
					diritem.Name = sc.Service.Name
					diritem.ID = own.Req.NewID()
					if ititle, ok := sc.Service.Instance.(types.ITitle); ok {
						diritem.Title = ititle.GetTitle()
					}
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
			cmds := strings.Split(info.Path, "/")
			if len(cmds) > 0 {
				cmd := cmds[len(cmds)-1]
				if cmd != "" {
					npm := smodels.NewPermissionsModel()
					npm.Name = cmd
					npm.Url = info.Path
					item.Permissions = append(item.Permissions, npm)
				}
			}
		}
	}
	return items
}
func getMenuModel(info *types.RouterInfo, items []*smodels.MenuModel) *smodels.MenuModel {
	name := strings.ToLower(info.InstanceName)
	endIndex := strings.Index(info.Path, name)
	url := info.Path[0:endIndex] + name
	for _, item := range items {
		if item.Name == info.InstanceName && item.Url == url {
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
	if ms, ok := own.GetInstance().(manage.IRequestSet); ok {
		ms.SetReq(req)
	}
	return nil
}
func (own *UpdateMenu) Do(req types.IRequest) (interface{}, error) {
	if own.GetInstance() == nil {
		return nil, errors.New("UpdateMenu instance is nil")
	}
	if mm, ok := own.GetInstance().(*MenuManage); ok {
		mm.updateMenuModelAll()
	}
	return nil, nil
}
func (own *UpdateMenu) RouterInfo() *types.RouterInfo {
	return manage.RouterInfo(own)
}
