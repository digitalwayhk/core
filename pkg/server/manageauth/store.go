// 本文件通过 Core 系统模型按请求查询自定义角色及其精确权限。
package manageauth

import (
	"context"
	"errors"
	"sync"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/smodels"
)

type manageRoleList interface {
	SearchWhere(string, interface{}, ...func(*persistencetype.SearchItem)) ([]*smodels.ManageRoleModel, error)
}

type manageRolePermissionList interface {
	SearchWhere(string, interface{}, ...func(*persistencetype.SearchItem)) ([]*smodels.ManageRolePermissionModel, error)
}

// ModelStore 通过标准持久化边界查询 Core 系统角色模型。
type ModelStore struct {
	action            persistencetype.IDataAction
	transactionMu     sync.Mutex
	newRoleList       func() manageRoleList
	newPermissionList func() manageRolePermissionList
}

// NewModelStore 创建绑定到控制面 action 的模型权限存储。
func NewModelStore(action persistencetype.IDataAction) *ModelStore {
	store := newModelStoreWithLists(
		func() manageRoleList {
			return entity.NewModelList[smodels.ManageRoleModel](cloneDataAction(action))
		},
		func() manageRolePermissionList {
			return entity.NewModelList[smodels.ManageRolePermissionModel](cloneDataAction(action))
		},
	)
	store.action = action
	return store
}

// FindPrincipal 按稳定管理员 Code 查询 Core 控制面主体。
func (own *ModelStore) FindPrincipal(ctx context.Context, code string) (*smodels.ManagePrincipalModel, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if own == nil || own.action == nil {
		return nil, errors.New("manage principal store is unavailable")
	}
	return entity.NewModelList[smodels.ManagePrincipalModel](cloneDataAction(own.action)).SearchOne(
		func(search *persistencetype.SearchItem) { search.AddWhereN("Code", code) },
	)
}

// ListPrincipalRoles 返回管理员绑定的全部稳定 RoleCode 关系。
func (own *ModelStore) ListPrincipalRoles(ctx context.Context, code string) ([]*smodels.ManagePrincipalRoleModel, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if own == nil || own.action == nil {
		return nil, errors.New("manage principal store is unavailable")
	}
	return entity.NewModelList[smodels.ManagePrincipalRoleModel](cloneDataAction(own.action)).SearchWhere("PrincipalCode", code)
}

// CreatePrincipalWithRole 在同一事务中创建管理员主体和初始角色关系。
func (own *ModelStore) CreatePrincipalWithRole(
	ctx context.Context,
	principal *smodels.ManagePrincipalModel,
	roleCode string,
	bootstrap bool,
) (err error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if own == nil || own.action == nil {
		return errors.New("manage principal store is unavailable")
	}
	if principal == nil {
		return errors.New("manage principal is required")
	}
	if bootstrap {
		slot := smodels.ManagePrincipalBootstrapSlot
		principal.IsFirst, principal.BootstrapSlot = true, &slot
	} else {
		principal.IsFirst, principal.BootstrapSlot = false, nil
	}

	own.transactionMu.Lock()
	defer own.transactionMu.Unlock()
	cloner, ok := own.action.(interface {
		Clone() persistencetype.IDataAction
	})
	if !ok {
		return errors.New("manage principal store does not support isolated transactions")
	}
	transaction := cloner.Clone()
	if transaction == nil {
		return errors.New("manage principal transaction is unavailable")
	}
	if err := transaction.Transaction(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			err = errors.Join(err, transaction.Rollback())
		}
	}()

	principals := entity.NewModelList[smodels.ManagePrincipalModel](transaction)
	if err := principals.Add(principal); err != nil {
		return err
	}
	if err := principals.Save(); err != nil {
		return err
	}
	relation := smodels.NewManagePrincipalRoleModel()
	relation.PrincipalCode, relation.RoleCode = principal.Code, roleCode
	relation.IsBootstrap = bootstrap
	relations := entity.NewModelList[smodels.ManagePrincipalRoleModel](transaction)
	if err := relations.Add(relation); err != nil {
		return err
	}
	if err := relations.Save(); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func cloneDataAction(action persistencetype.IDataAction) persistencetype.IDataAction {
	if cloner, ok := action.(interface {
		Clone() persistencetype.IDataAction
	}); ok {
		return cloner.Clone()
	}
	return action
}

func newModelStoreWithLists(
	roles func() manageRoleList,
	permissions func() manageRolePermissionList,
) *ModelStore {
	return &ModelStore{newRoleList: roles, newPermissionList: permissions}
}

// FindRole 按稳定 RoleCode 查询角色。
func (own *ModelStore) FindRole(_ context.Context, code string) (*smodels.ManageRoleModel, error) {
	if own == nil || own.newRoleList == nil {
		return nil, nil
	}
	items, err := own.newRoleList().SearchWhere("Code", code)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

// HasPermission 精确匹配 RoleCode、service、path 与 command。
func (own *ModelStore) HasPermission(
	_ context.Context,
	roleCode string,
	service string,
	path string,
	command string,
) (bool, error) {
	if own == nil || own.newPermissionList == nil {
		return false, nil
	}
	items, err := own.newPermissionList().SearchWhere("RoleCode", roleCode, func(search *persistencetype.SearchItem) {
		search.AddWhereN("Service", service)
		search.AddWhereN("Path", path)
		search.AddWhereN("Command", command)
		search.Size = 1
	})
	if err != nil {
		return false, err
	}
	return len(items) > 0, nil
}
