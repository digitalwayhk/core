package adminrbac

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/digitalwayhk/core/examples/09-admin-manage-rbac/models"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
)

type adminRepository interface {
	FindUser(context.Context, string) (*models.AdminUserModel, error)
	CreateUser(context.Context, *models.AdminUserModel) ([]servertype.ManageRoleRef, error)
	RoleCodes(context.Context, string) ([]servertype.ManageRoleRef, error)
}

// ManageRoleProvider 演示消费方如何把 Casdoor 身份映射到自己的管理员与 RoleCode 关系。
type ManageRoleProvider struct {
	repository adminRepository
	bootstrap  sync.Mutex
}

func NewManageRoleProvider(repository adminRepository) *ManageRoleProvider {
	return &ManageRoleProvider{repository: repository}
}

func (own *ManageRoleProvider) ResolveManagePrincipal(
	ctx context.Context,
	request servertype.ManagePrincipalRequest,
) (servertype.ManagePrincipal, error) {
	if own == nil || own.repository == nil {
		return servertype.ManagePrincipal{}, errors.New("administrator repository unavailable")
	}
	identity := request.Identity
	identity.UID = strings.TrimSpace(identity.UID)
	identity.ProviderSubject = strings.TrimSpace(identity.ProviderSubject)
	if identity.AuthType != servertype.AuthTypeManage || identity.Provider != servertype.AuthProviderCasdoor ||
		identity.UID == "" || identity.ProviderSubject == "" {
		return servertype.ManagePrincipal{}, errors.New("trusted Casdoor Manage identity is required")
	}
	if request.Source != servertype.AuthSourceCallback && request.Source != servertype.AuthSourceRefresh {
		return servertype.ManagePrincipal{}, errors.New("unsupported manage principal source")
	}

	own.bootstrap.Lock()
	defer own.bootstrap.Unlock()
	user, err := own.repository.FindUser(ctx, identity.UID)
	if err != nil {
		return servertype.ManagePrincipal{}, err
	}
	if user == nil {
		if request.Source != servertype.AuthSourceCallback {
			return servertype.ManagePrincipal{}, errors.New("administrator does not exist")
		}
		user = models.NewAdminUserModel()
		user.Code = identity.UID
		user.Username = identity.Username
		user.Provider = identity.Provider
		user.ProviderSubject = identity.ProviderSubject
		user.Enabled = true
		if _, err := own.repository.CreateUser(ctx, user); err != nil {
			return servertype.ManagePrincipal{}, err
		}
	}
	if !user.Enabled || user.Provider != identity.Provider || user.ProviderSubject != identity.ProviderSubject {
		return servertype.ManagePrincipal{}, errors.New("administrator identity is disabled or mismatched")
	}
	roles, err := own.repository.RoleCodes(ctx, user.Code)
	if err != nil {
		return servertype.ManagePrincipal{}, err
	}
	roles, err = servertype.NormalizeManageRoleRefs(roles)
	if err != nil || len(roles) == 0 {
		if err != nil {
			return servertype.ManagePrincipal{}, err
		}
		return servertype.ManagePrincipal{}, errors.New("administrator has no roles")
	}
	return servertype.ManagePrincipal{Roles: roles}, nil
}

type memoryAdminRepository struct {
	mu    sync.Mutex
	users map[string]*models.AdminUserModel
	roles map[string][]servertype.ManageRoleRef
}

func newMemoryAdminRepository() *memoryAdminRepository {
	return &memoryAdminRepository{
		users: make(map[string]*models.AdminUserModel),
		roles: make(map[string][]servertype.ManageRoleRef),
	}
}

func (own *memoryAdminRepository) FindUser(_ context.Context, code string) (*models.AdminUserModel, error) {
	own.mu.Lock()
	defer own.mu.Unlock()
	return own.users[code], nil
}

func (own *memoryAdminRepository) CreateUser(_ context.Context, user *models.AdminUserModel) ([]servertype.ManageRoleRef, error) {
	own.mu.Lock()
	defer own.mu.Unlock()
	if _, exists := own.users[user.Code]; exists {
		return nil, errors.New("administrator already exists")
	}
	roles := []servertype.ManageRoleRef{{Code: servertype.ManageRoleViewer}}
	user.IsFirst = len(own.users) == 0
	if user.IsFirst {
		roles = []servertype.ManageRoleRef{{Code: servertype.ManageRoleSystemAdmin}}
	}
	own.users[user.Code] = user
	own.roles[user.Code] = append([]servertype.ManageRoleRef(nil), roles...)
	return roles, nil
}

func (own *memoryAdminRepository) RoleCodes(_ context.Context, code string) ([]servertype.ManageRoleRef, error) {
	own.mu.Lock()
	defer own.mu.Unlock()
	return append([]servertype.ManageRoleRef(nil), own.roles[code]...), nil
}

func (own *memoryAdminRepository) userCount() int {
	own.mu.Lock()
	defer own.mu.Unlock()
	return len(own.users)
}
