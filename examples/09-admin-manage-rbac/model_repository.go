package adminrbac

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/digitalwayhk/core/examples/09-admin-manage-rbac/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
)

type modelAdminRepository struct {
	mu sync.Mutex
}

func newModelAdminRepository() *modelAdminRepository { return &modelAdminRepository{} }

func (*modelAdminRepository) FindUser(_ context.Context, code string) (*models.AdminUserModel, error) {
	list := entity.NewModelList[models.AdminUserModel](nil)
	return list.SearchOne(func(search *persistencetype.SearchItem) {
		search.AddWhereN("Code", code)
	})
}

func (*modelAdminRepository) RoleCodes(_ context.Context, userCode string) ([]servertype.ManageRoleRef, error) {
	list := entity.NewModelList[models.AdminUserRoleModel](nil)
	items, err := list.SearchWhere("UserCode", userCode)
	if err != nil {
		return nil, err
	}
	roles := make([]servertype.ManageRoleRef, 0, len(items))
	for _, item := range items {
		if item != nil {
			roles = append(roles, servertype.ManageRoleRef{Code: item.RoleCode})
		}
	}
	return roles, nil
}

func (own *modelAdminRepository) CreateUser(
	_ context.Context,
	user *models.AdminUserModel,
) (_ []servertype.ManageRoleRef, err error) {
	own.mu.Lock()
	defer own.mu.Unlock()
	if user == nil {
		return nil, errors.New("administrator is required")
	}

	// 首次查询按 Core 规则自动建表；不使用 migration 或业务 AutoMigrate。
	prepareUsers := entity.NewModelList[models.AdminUserModel](nil)
	if _, err := prepareUsers.SearchOne(); err != nil {
		return nil, fmt.Errorf("prepare administrators: %w", err)
	}
	action := prepareUsers.GetAction()
	if action == nil {
		return nil, errors.New("administrator persistence unavailable")
	}
	prepareRoles := entity.NewModelList[models.AdminUserRoleModel](action)
	if _, err := prepareRoles.SearchOne(); err != nil {
		return nil, fmt.Errorf("prepare administrator roles: %w", err)
	}
	if err := action.Transaction(); err != nil {
		return nil, fmt.Errorf("begin administrator bootstrap: %w", err)
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

	users := entity.NewModelList[models.AdminUserModel](action)
	existing, err := users.SearchOne(func(search *persistencetype.SearchItem) {
		search.AddWhereN("Code", user.Code)
	})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("administrator already exists")
	}
	_, total, err := users.SearchAll(1, 1)
	if err != nil {
		return nil, err
	}
	user.IsFirst = total == 0
	roles := []servertype.ManageRoleRef{{Code: servertype.ManageRoleViewer}}
	if user.IsFirst {
		roles = []servertype.ManageRoleRef{{Code: servertype.ManageRoleSystemAdmin}}
	}
	if err := users.Add(user); err != nil {
		return nil, err
	}
	if err := users.Save(); err != nil {
		return nil, err
	}

	relations := entity.NewModelList[models.AdminUserRoleModel](action)
	for _, role := range roles {
		relation := models.NewAdminUserRoleModel()
		relation.UserCode = user.Code
		relation.RoleCode = role.Code
		if err := relations.Add(relation); err != nil {
			return nil, err
		}
	}
	if err := relations.Save(); err != nil {
		return nil, err
	}
	if err := action.Commit(); err != nil {
		return nil, fmt.Errorf("commit administrator bootstrap: %w", err)
	}
	committed = true
	return roles, nil
}
