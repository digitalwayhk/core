// 本文件提供角色权限树的读取与事务保存；权限身份仍使用 RoleCode + Service + Path + Command。
package manage

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	st "github.com/digitalwayhk/core/pkg/server/types"
	m "github.com/digitalwayhk/core/service/manage"
)

type rolePermissionInput struct {
	RoleCode    string               `json:"roleCode"`
	Permissions []permissionIdentity `json:"permissions"`
}

type permissionIdentity struct {
	Service string `json:"service"`
	Path    string `json:"path"`
	Command string `json:"command"`
}

// PermissionSelection 返回指定自定义角色已保存的精确权限。
type PermissionSelection struct {
	m.Operation[smodels.ManageRolePermissionModel]
	input rolePermissionInput
}

func NewPermissionSelection(instance interface{}) *PermissionSelection {
	return &PermissionSelection{Operation: m.NewOperation[smodels.ManageRolePermissionModel](instance)}
}
func (*PermissionSelection) New(instance interface{}) st.IRouter {
	return NewPermissionSelection(instance)
}
func (own *PermissionSelection) Parse(req st.IRequest) error { return req.Bind(&own.input) }
func (own *PermissionSelection) Validation(st.IRequest) error {
	_, err := selectionRole(own.input.RoleCode)
	return err
}
func (own *PermissionSelection) Do(st.IRequest) (interface{}, error) {
	return loadRolePermissions(smodels.NewManageModelList[smodels.ManageRolePermissionModel]().GetAction(), strings.ToLower(strings.TrimSpace(own.input.RoleCode)))
}
func (own *PermissionSelection) RouterInfo() *st.RouterInfo { return m.RouterInfo(own) }

// SavePermissionSelection 用当前勾选集替换该角色权限，支持取消全部授权。
type SavePermissionSelection struct {
	m.Operation[smodels.ManageRolePermissionModel]
	input        rolePermissionInput
	hasSelection bool
}

func NewSavePermissionSelection(instance interface{}) *SavePermissionSelection {
	return &SavePermissionSelection{Operation: m.NewOperation[smodels.ManageRolePermissionModel](instance)}
}
func (*SavePermissionSelection) New(instance interface{}) st.IRouter {
	return NewSavePermissionSelection(instance)
}
func (own *SavePermissionSelection) Parse(req st.IRequest) error {
	var input struct {
		RoleCode    string                `json:"roleCode"`
		Permissions *[]permissionIdentity `json:"permissions"`
	}
	if err := req.Bind(&input); err != nil {
		return err
	}
	own.input.RoleCode = input.RoleCode
	own.hasSelection = input.Permissions != nil
	if own.hasSelection {
		own.input.Permissions = *input.Permissions
	}
	return nil
}
func (own *SavePermissionSelection) Validation(st.IRequest) error {
	if !own.hasSelection {
		return invalidRoleBinding("permissions selection is required")
	}
	if len(own.input.Permissions) > 10000 {
		return invalidRoleBinding("too many permissions")
	}
	_, err := selectionRole(own.input.RoleCode)
	return err
}
func (own *SavePermissionSelection) Do(req st.IRequest) (interface{}, error) {
	role, err := selectionRole(own.input.RoleCode)
	if err != nil {
		return nil, err
	}
	snapshots, err := discoverMenuServiceSnapshots(req)
	if err != nil {
		return nil, err
	}
	if sc := router.GetContext("server"); sc != nil {
		snapshots = append(snapshots, public.NewMenuServiceSnapshot(sc))
	}
	allowed := make(map[permissionIdentity]bool)
	for _, snapshot := range snapshots {
		for _, route := range snapshot.Routers {
			allowed[permissionIdentity{Service: snapshot.Name, Path: route.Path, Command: route.Command}] = true
		}
	}
	rows := make([]*smodels.ManageRolePermissionModel, 0, len(own.input.Permissions))
	seen := make(map[permissionIdentity]bool)
	for _, identity := range own.input.Permissions {
		if !allowed[identity] {
			return nil, invalidRoleBinding("permission is not a registered Manage command")
		}
		if seen[identity] {
			continue
		}
		seen[identity] = true
		row := smodels.NewManageRolePermissionModel()
		row.RoleCode, row.Service, row.Path, row.Command = role.Code, identity.Service, identity.Path, identity.Command
		if err := row.AddValid(); err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	action := smodels.NewManageModelList[smodels.ManageRolePermissionModel]().GetAction()
	if err := replaceRolePermissions(action, role.Code, rows); err != nil {
		return nil, err
	}
	return rows, nil
}
func (own *SavePermissionSelection) RouterInfo() *st.RouterInfo { return m.RouterInfo(own) }

func selectionRole(code string) (*smodels.ManageRoleModel, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if err := st.ValidateManageRoleCode(code); err != nil {
		return nil, invalidRoleBinding(err.Error())
	}
	role, err := (&modelManageRoleBindingStore{}).FindRole(nil, code)
	if err != nil {
		return nil, err
	}
	if role == nil {
		return nil, st.NewPublicError(st.ErrorKindNotFound, 0, "manage role not found", nil)
	}
	if smodels.IsBuiltInManageRoleCode(code) || role.IsSystem || role.Policy != st.ManageRolePolicyExplicit {
		return nil, st.NewPublicError(st.ErrorKindForbidden, 0, "内置角色权限不可编辑", nil)
	}
	return role, nil
}

func replaceRolePermissions(action pt.IDataAction, code string, rows []*smodels.ManageRolePermissionModel) (err error) {
	if action == nil {
		return errors.New("role permission persistence unavailable")
	}
	if err := action.Transaction(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			err = errors.Join(err, action.Rollback())
		}
	}()
	old, err := loadRolePermissions(action, code)
	if err != nil {
		return err
	}
	for _, row := range old {
		if err := action.Delete(row); err != nil {
			return err
		}
	}
	for _, row := range rows {
		row.SetHashcode(row.GetHash())
		if err := action.Insert(row); err != nil {
			return err
		}
	}
	if err := action.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// loadRolePermissions 完整读取后再替换，避免默认500条上限遗漏回显或撤权。
func loadRolePermissions(action pt.IDataAction, code string) ([]*smodels.ManageRolePermissionModel, error) {
	list := entity.NewModelList[smodels.ManageRolePermissionModel](action)
	var rows []*smodels.ManageRolePermissionModel
	for page := 1; ; page++ {
		batch, _, err := list.SearchAll(page, 500, func(item *pt.SearchItem) {
			item.AddWhereN("RoleCode", code)
			item.AddSortN("ID", false)
			item.SkipCount = true
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
		if len(batch) < 500 {
			return rows, nil
		}
	}
}

// RolePermissions 是选中角色的页面入口，权限编辑器使用独立的读写 API。
type RolePermissions struct {
	m.Operation[smodels.ManageRoleModel]
}

func NewRolePermissions(instance interface{}) *RolePermissions {
	return &RolePermissions{Operation: m.NewOperation[smodels.ManageRoleModel](instance)}
}
func (*RolePermissions) New(instance interface{}) st.IRouter { return NewRolePermissions(instance) }
func (*RolePermissions) Do(st.IRequest) (interface{}, error) { return nil, nil }
func (own *RolePermissions) RouterInfo() *st.RouterInfo      { return m.RouterInfo(own) }
