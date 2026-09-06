package manage

import (
	"errors"
	"fmt"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
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

// SearchAfter 在菜单空表时走 UpdateMenu 的原子持久化链路，避免通用默认数据保存直接写入关联权限。
func (own *MenuManage) SearchAfter(sender interface{}, result *view.TableData, req types.IRequest) (interface{}, error) {
	if !shouldBootstrapMenuSearch(sender, result) {
		return result, nil
	}
	if err := own.updateMenuModelAll(req); err != nil {
		return nil, err
	}
	list, ok := own.GetList().(*entity.ModelList[smodels.MenuModel])
	if !ok || list == nil {
		return nil, errors.New("MenuManage list unavailable")
	}
	search := sender.(*manage.Search[smodels.MenuModel])
	rows, total, err := list.SearchAll(1, search.SearchItem.Size)
	if err != nil {
		return nil, err
	}
	result.Rows = rows
	result.Total = total
	return result, nil
}

func shouldBootstrapMenuSearch(sender interface{}, result *view.TableData) bool {
	if result == nil || result.Total != 0 {
		return false
	}
	search, ok := sender.(*manage.Search[smodels.MenuModel])
	return ok && search.SearchItem != nil && search.SearchItem.Page <= 1 && len(search.SearchItem.WhereList) == 0
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
	snapshots, err := discoverMenuServiceSnapshots(req)
	if err != nil {
		return err
	}
	items, err := own.defaultItemsFromSnapshots(req, snapshots)
	if err != nil {
		return err
	}
	if err := syncMenusAtomic(action, items); err != nil {
		return err
	}
	// 存量目录的标题同样以代码为权威源，需要随服务实例的中英文案一起刷新
	if err := syncDirectoryTitles(action, generatedDirectoryTitlesFromSnapshots(snapshots)); err != nil {
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
	snapshots, err := discoverMenuServiceSnapshots(req)
	if err != nil {
		logx.Errorw("menu_discovery_failed", logx.Field("error", err))
		return nil
	}
	items, err := own.defaultItemsFromSnapshots(req, snapshots)
	if err != nil {
		logx.Errorw("menu_build_failed", logx.Field("error", err))
		return nil
	}
	return items
}

func (own *MenuManage) defaultItemsFromSnapshots(
	req types.IRequest,
	snapshots []*smodels.MenuServiceSnapshot,
) ([]*smodels.MenuModel, error) {
	items := make([]*smodels.MenuModel, 0)
	dir := NewDirectoryManage()
	dirList := dir.GetList().(*entity.ModelList[smodels.DirectoryModel])
	for _, snapshot := range snapshots {
		if snapshot == nil || !snapshotHasMenu(snapshot) {
			continue
		}
		dirID, err := own.ensureServiceDirectoryID(req, dirList, snapshot)
		if err != nil {
			return nil, err
		}
		items = append(items, buildMenuModelsForService(snapshot, dirID)...)
	}
	return items, nil
}

func snapshotHasMenu(snapshot *smodels.MenuServiceSnapshot) bool {
	if snapshot == nil {
		return false
	}
	if len(snapshot.Reports) > 0 {
		return true
	}
	for _, info := range snapshot.Routers {
		if info.Path != "" && !isReportAPIPath(info.Path) {
			return true
		}
	}
	return false
}

// buildMenuModelsForService 把单个服务快照中的 Manage 路由按控制器合并成菜单和权限。
func buildMenuModelsForService(snapshot *smodels.MenuServiceSnapshot, dirID uint) []*smodels.MenuModel {
	if snapshot == nil {
		return nil
	}
	items := make([]*smodels.MenuModel, 0)
	reportAPIPaths := make([]string, 0)
	for _, info := range snapshot.Routers {
		path := info.Path
		if path == "" {
			continue
		}
		if isReportAPIPath(path) {
			reportAPIPaths = append(reportAPIPaths, path)
			continue
		}
		item := getMenuModel(info, items)
		if item == nil {
			item = smodels.NewMenuModel()
			item.Name = info.InstanceName
			item.Title = info.Title
			if item.Title == "" {
				item.Title = item.Name
			}
			item.TitleEN = info.TitleEN
			item.Url = buildMenuUrl(path, strings.ToLower(item.Name))
			item.Permissions = make([]*smodels.PermissionsModel, 0)
			item.DirectoryModelID = dirID
			items = append(items, item)
		}
		cmd := pathLastSegment(path)
		if cmd != "" {
			npm := smodels.NewPermissionsModel()
			npm.Name = cmd
			npm.Url = path
			item.Permissions = append(item.Permissions, npm)
		}
	}
	return append(items, buildReportMenuItems(snapshot.Reports, reportAPIPaths, dirID)...)
}

// buildReportMenuItems 将目标服务快照中的报表定义展开为菜单行（与 Dashboard 分析页分离）。
func buildReportMenuItems(
	defs []stats.ReportMenuItem,
	reportAPIPaths []string,
	dirID uint,
) []*smodels.MenuModel {
	if len(defs) == 0 {
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
	snapshot *smodels.MenuServiceSnapshot,
) (uint, error) {
	if dirList == nil || snapshot == nil || snapshot.Name == "" {
		return 0, errors.New("menu service directory unavailable")
	}
	dirrows, err := dirList.SearchName(snapshot.Name)
	if err != nil {
		return 0, fmt.Errorf("search menu service directory %s: %w", snapshot.Name, err)
	}
	if len(dirrows) > 0 {
		return dirrows[0].ID, nil
	}
	if req == nil {
		return 0, errors.New("menu service directory request unavailable")
	}
	diritem := own.newDirectoryModel(req, snapshot)
	if err := dirList.Add(diritem); err != nil {
		return 0, fmt.Errorf("add menu service directory %s: %w", snapshot.Name, err)
	}
	if err := dirList.Save(); err != nil {
		return 0, fmt.Errorf("save menu service directory %s: %w", snapshot.Name, err)
	}
	return diritem.ID, nil
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

func (own *MenuManage) newDirectoryModel(req types.IRequest, snapshot *smodels.MenuServiceSnapshot) *smodels.DirectoryModel {
	diritem := smodels.NewDirectoryModel()
	diritem.Name = snapshot.Name
	diritem.ID = req.NewID()
	diritem.Title = snapshot.Title
	if diritem.Title == "" {
		diritem.Title = diritem.Name
	}
	diritem.TitleEN = snapshot.TitleEN
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

func getMenuModel(info smodels.MenuRouterSnapshot, items []*smodels.MenuModel) *smodels.MenuModel {
	instanceName := info.InstanceName
	path := info.Path
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
