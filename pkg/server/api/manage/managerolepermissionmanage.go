// 本文件提供自定义角色精确权限页面及 view/search 菜单默认绑定操作。
package manage

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ManageRolePermissionManage 提供自定义角色精确权限及菜单默认权限绑定。
type ManageRolePermissionManage struct {
	*DmpBase[smodels.ManageRolePermissionModel]
	bindingStore manageRoleBindingStore
}

// NewManageRolePermissionManage 创建角色权限 Manage 页面。
func NewManageRolePermissionManage() *ManageRolePermissionManage {
	own := &ManageRolePermissionManage{}
	own.DmpBase = NewDmpBase[smodels.ManageRolePermissionModel](own)
	own.bindingStore = &modelManageRoleBindingStore{}
	return own
}

// Routers 提供精确权限 CRUD 与菜单默认权限绑定。
func (own *ManageRolePermissionManage) Routers() []servertype.IRouter {
	routers := own.DmpBase.Routers()
	routers = append(routers, own.Add, own.Remove, NewBindMenu(own))
	return routers
}

// ViewCommandModel 配置菜单默认权限绑定命令。
func (own *ManageRolePermissionManage) ViewCommandModel(command *view.CommandModel) {
	if command.Command == "bindmenu" {
		command.Title = "绑定菜单默认权限"
		command.EditShow = true
		command.IsSelectRow = false
		command.IsAlert = false
	}
}

// ViewFieldModel 配置结构化权限字段。
func (own *ManageRolePermissionManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.DmpBase.ViewFieldModel(model, field)
	switch {
	case field.IsFieldOrTitle("rolecode"):
		field.Title = "角色编码"
		field.Required = true
	case field.IsFieldOrTitle("service"):
		field.Title = "服务"
		field.Required = true
	case field.IsFieldOrTitle("path"):
		field.Title = "路由或菜单路径"
		field.Required = true
	case field.IsFieldOrTitle("command"):
		field.Title = "命令"
	}
}

// ManageRoleMenuBinding 使用权限模型表单中的 RoleCode、Service 和 Path 绑定菜单。
type ManageRoleMenuBinding struct {
	RoleCode string
	Service  string
	MenuPath string
}

// BindMenu 为自定义角色幂等绑定菜单的 view/search 权限。
type BindMenu struct {
	manageservice.Operation[smodels.ManageRolePermissionModel]
}

// NewBindMenu 创建菜单默认权限绑定操作。
func NewBindMenu(instance interface{}) *BindMenu {
	return &BindMenu{Operation: manageservice.NewOperation[smodels.ManageRolePermissionModel](instance)}
}

// New 创建请求级 BindMenu 操作实例。
func (own *BindMenu) New(instance interface{}) servertype.IRouter {
	return NewBindMenu(instance)
}

// Validation 校验表单中的角色、服务和菜单路径。
func (own *BindMenu) Validation(servertype.IRequest) error {
	if own.Model == nil {
		return invalidRoleBinding("binding input is required")
	}
	return validateManageRoleMenuBinding(ManageRoleMenuBinding{
		RoleCode: own.Model.RoleCode,
		Service:  own.Model.Service,
		MenuPath: own.Model.Path,
	})
}

// Do 执行幂等菜单默认权限绑定。
func (own *BindMenu) Do(_ servertype.IRequest) (interface{}, error) {
	manager, ok := own.GetInstance().(*ManageRolePermissionManage)
	if !ok || manager == nil || manager.bindingStore == nil {
		return nil, servertype.NewPublicError(
			servertype.ErrorKindInternal, 0, "", errors.New("manage role binding store unavailable"),
		)
	}
	return bindDefaultMenuPermissions(context.Background(), manager.bindingStore, ManageRoleMenuBinding{
		RoleCode: own.Model.RoleCode,
		Service:  own.Model.Service,
		MenuPath: own.Model.Path,
	})
}

// RouterInfo 返回标准 Manage 自定义命令元数据。
func (own *BindMenu) RouterInfo() *servertype.RouterInfo { return manageservice.RouterInfo(own) }

type manageRoleBindingStore interface {
	FindRole(context.Context, string) (*smodels.ManageRoleModel, error)
	FindMenu(context.Context, string) (*smodels.MenuModel, error)
	EnsurePermissions(context.Context, []*smodels.ManageRolePermissionModel) error
}

func bindDefaultMenuPermissions(
	ctx context.Context,
	store manageRoleBindingStore,
	binding ManageRoleMenuBinding,
) ([]*smodels.ManageRolePermissionModel, error) {
	if err := validateManageRoleMenuBinding(binding); err != nil {
		return nil, err
	}
	role, err := store.FindRole(ctx, strings.ToLower(strings.TrimSpace(binding.RoleCode)))
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, servertype.NewPublicError(
			servertype.ErrorKindNotFound, 0, "manage role not found", errors.New("manage role binding role not found"),
		)
	}
	if smodels.IsBuiltInManageRoleCode(role.Code) || role.IsSystem {
		return nil, servertype.NewPublicError(
			servertype.ErrorKindForbidden, 0, "", errors.New("built-in role permissions are dynamic"),
		)
	}
	if !role.Enabled || role.Policy != servertype.ManageRolePolicyExplicit {
		return nil, servertype.NewPublicError(
			servertype.ErrorKindForbidden, 0, "", errors.New("manage role is not enabled for explicit permissions"),
		)
	}
	menu, err := store.FindMenu(ctx, strings.TrimSpace(binding.MenuPath))
	if err != nil {
		return nil, err
	}
	if menu == nil {
		return nil, servertype.NewPublicError(
			servertype.ErrorKindNotFound, 0, "manage menu not found", errors.New("manage role binding menu not found"),
		)
	}
	rows := defaultMenuPermissionRows(role.Code, binding.Service, menu)
	if len(rows) == 0 {
		return nil, invalidRoleBinding("menu does not expose view or search")
	}
	if err := store.EnsurePermissions(ctx, rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func validateManageRoleMenuBinding(binding ManageRoleMenuBinding) error {
	roleCode := strings.ToLower(strings.TrimSpace(binding.RoleCode))
	service := strings.ToLower(strings.TrimSpace(binding.Service))
	menuPath := strings.TrimSpace(binding.MenuPath)
	if err := servertype.ValidateManageRoleCode(roleCode); err != nil {
		return invalidRoleBinding(err.Error())
	}
	if service == "" || service != strings.TrimSpace(binding.Service) {
		return invalidRoleBinding("service is invalid")
	}
	if !strings.HasPrefix(menuPath, "/api/manage/"+service+"/") {
		return invalidRoleBinding("menu path does not belong to service")
	}
	return nil
}

func defaultMenuPermissionRows(
	roleCode string,
	service string,
	menu *smodels.MenuModel,
) []*smodels.ManageRolePermissionModel {
	if menu == nil {
		return nil
	}
	byCommand := make(map[string]*smodels.PermissionsModel, len(menu.Permissions))
	for _, permission := range menu.Permissions {
		if permission == nil {
			continue
		}
		command := strings.ToLower(strings.TrimSpace(permission.Name))
		if command == "view" || command == "search" {
			byCommand[command] = permission
		}
	}
	rows := make([]*smodels.ManageRolePermissionModel, 0, 2)
	for _, command := range []string{"view", "search"} {
		permission := byCommand[command]
		if permission == nil || strings.TrimSpace(permission.Url) == "" {
			continue
		}
		row := smodels.NewManageRolePermissionModel()
		row.RoleCode = strings.ToLower(strings.TrimSpace(roleCode))
		row.Service = strings.ToLower(strings.TrimSpace(service))
		row.Path = strings.TrimSpace(permission.Url)
		row.Command = command
		rows = append(rows, row)
	}
	return rows
}

type modelManageRoleBindingStore struct{}

func (*modelManageRoleBindingStore) FindRole(_ context.Context, code string) (*smodels.ManageRoleModel, error) {
	list := entity.NewModelList[smodels.ManageRoleModel](nil)
	return list.SearchOne(func(search *persistencetype.SearchItem) {
		search.AddWhereN("Code", code)
	})
}

func (*modelManageRoleBindingStore) FindMenu(_ context.Context, path string) (*smodels.MenuModel, error) {
	list := entity.NewModelList[smodels.MenuModel](nil)
	return list.SearchOne(func(search *persistencetype.SearchItem) {
		search.AddWhereN("Url", path)
	})
}

func (*modelManageRoleBindingStore) EnsurePermissions(
	_ context.Context,
	rows []*smodels.ManageRolePermissionModel,
) (err error) {
	list := entity.NewModelList[smodels.ManageRolePermissionModel](nil)
	probe := list.GetSearchItem()
	probe.Model = smodels.NewManageRolePermissionModel()
	action := list.GetDBAdapter(probe)
	if action == nil {
		return errors.New("manage role permission persistence unavailable")
	}
	// 首次 Load 负责按 Core 规则自动建表，再开始显式事务。
	if _, err := list.SearchOne(); err != nil {
		return fmt.Errorf("prepare manage role permissions: %w", err)
	}
	if err := action.Transaction(); err != nil {
		return fmt.Errorf("begin manage role permission transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := action.Rollback(); rollbackErr != nil {
			err = errors.Join(err, rollbackErr)
		}
	}()

	transactionList := entity.NewModelList[smodels.ManageRolePermissionModel](action)
	for _, row := range rows {
		if row == nil {
			continue
		}
		existing, searchErr := transactionList.SearchOne(func(search *persistencetype.SearchItem) {
			search.AddWhereN("RoleCode", row.RoleCode)
			search.AddWhereN("Service", row.Service)
			search.AddWhereN("Path", row.Path)
			search.AddWhereN("Command", row.Command)
		})
		if searchErr != nil {
			return searchErr
		}
		if existing == nil {
			if err := transactionList.Add(row); err != nil {
				return err
			}
		}
	}
	if err := transactionList.Save(); err != nil {
		return err
	}
	if err := action.Commit(); err != nil {
		return fmt.Errorf("commit manage role permission transaction: %w", err)
	}
	committed = true
	return nil
}

func invalidRoleBinding(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindValidation, 0, "invalid manage role binding", errors.New(detail),
	)
}
