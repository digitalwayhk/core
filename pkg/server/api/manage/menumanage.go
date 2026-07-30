package manage

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
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
	if err := syncMenusAtomic(action, own.GetDefaultItemsWithRequest(req)); err != nil {
		return err
	}
	// 历史 UpdateMenu 曾把 reports.List/View 扫成菜单行，同步后删除这些 API 伪菜单
	return removeStaleReportAPIMenus(action)
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
	// 各服务报表 API 路径（List/View），不单独成菜单行，挂到每张报表菜单的权限上
	reportAPIsByService := map[string][]string{}

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
			// 报表目录/视图 API 不是导航菜单，避免出现 List/View 两行
			if isReportAPIPath(path) {
				reportAPIsByService[sc.Service.Name] = append(reportAPIsByService[sc.Service.Name], path)
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
		// 一个 ReportDef 一行：挂到服务目录下，Url 为前端 /report/{service}/{code}
		items = append(items, own.buildReportMenuItems(req, dirList, sc, reportAPIsByService[sc.Service.Name])...)
	}
	return items
}

// buildReportMenuItems 将 stats 已注册的报表定义展开为菜单行（与 Dashboard 分析页分离）。
func (own *MenuManage) buildReportMenuItems(
	req types.IRequest,
	dirList *entity.ModelList[smodels.DirectoryModel],
	sc *router.ServiceContext,
	reportAPIPaths []string,
) []*smodels.MenuModel {
	if sc == nil || sc.Service == nil {
		return nil
	}
	defs := stats.ListReportMenus(sc.Service.Name)
	if len(defs) == 0 {
		return nil
	}
	dirID, ok := own.ensureServiceDirectoryID(req, dirList, sc)
	if !ok {
		return nil
	}
	perms := make([]*smodels.PermissionsModel, 0, len(reportAPIPaths))
	for _, p := range uniqueStrings(reportAPIPaths) {
		cmd := pathLastSegment(p)
		if cmd == "" {
			continue
		}
		npm := smodels.NewPermissionsModel()
		npm.Name = cmd
		npm.Url = p
		perms = append(perms, npm)
	}
	out := make([]*smodels.MenuModel, 0, len(defs))
	for _, m := range defs {
		item := smodels.NewMenuModel()
		item.Name = m.Code
		item.Title = m.Title
		item.Description = m.Description
		item.Url = m.Path
		item.Sort = m.Sort
		item.DirectoryModelID = dirID
		// 每张报表复用同一套 reports API 权限（读目录 + 读视图）
		item.Permissions = clonePermissions(perms)
		out = append(out, item)
	}
	return out
}

func (own *MenuManage) ensureServiceDirectoryID(
	req types.IRequest,
	dirList *entity.ModelList[smodels.DirectoryModel],
	sc *router.ServiceContext,
) (uint, bool) {
	if dirList == nil || sc == nil {
		return 0, false
	}
	dirrows, err := dirList.SearchName(sc.Service.Name)
	if err != nil {
		return 0, false
	}
	if len(dirrows) > 0 {
		return dirrows[0].ID, true
	}
	if req == nil {
		return 0, false
	}
	diritem := own.newDirectoryModel(req, sc)
	if err := dirList.Add(diritem); err != nil {
		logx.Errorf("Add directory model error: %v", err)
		return 0, false
	}
	if err := dirList.Save(); err != nil {
		logx.Errorf("Save directory model error: %v", err)
		return 0, false
	}
	return diritem.ID, true
}

// isReportAPIPath 识别报表数据 API（非前端页），UpdateMenu 不得为其生成 List/View 菜单行。
func isReportAPIPath(path string) bool {
	p := strings.TrimSuffix(strings.TrimSpace(path), "/")
	if p == "" {
		return false
	}
	return strings.HasSuffix(p, "/reports") || strings.HasSuffix(p, "/reports/view")
}

func pathLastSegment(path string) string {
	p := strings.TrimSuffix(strings.TrimSpace(path), "/")
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	return parts[len(parts)-1]
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func clonePermissions(src []*smodels.PermissionsModel) []*smodels.PermissionsModel {
	if len(src) == 0 {
		return []*smodels.PermissionsModel{}
	}
	out := make([]*smodels.PermissionsModel, 0, len(src))
	for _, p := range src {
		if p == nil {
			continue
		}
		npm := smodels.NewPermissionsModel()
		npm.Name = p.Name
		npm.Url = p.Url
		out = append(out, npm)
	}
	return out
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
