// 本文件通过 Core 系统模型按请求查询自定义角色及其精确权限。
package manageauth

import (
	"context"

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
	newRoleList       func() manageRoleList
	newPermissionList func() manageRolePermissionList
}

// NewModelStore 创建模型权限存储；nil action 使用 Core 默认系统数据库。
func NewModelStore(action persistencetype.IDataAction) *ModelStore {
	return newModelStoreWithLists(
		func() manageRoleList {
			return entity.NewModelList[smodels.ManageRoleModel](action)
		},
		func() manageRolePermissionList {
			return entity.NewModelList[smodels.ManageRolePermissionModel](action)
		},
	)
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
