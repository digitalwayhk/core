// 本文件把可信 Casdoor Manage 身份解析为 Core 控制面主体与 RoleCode。
package manageauth

import (
	"context"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
)

// PrincipalProvider 是 Core 默认的 Manage 管理员角色 Provider。
type PrincipalProvider struct {
	store *ModelStore
}

// NewPrincipalProvider 创建只访问 Core 控制面模型的角色 Provider。
func NewPrincipalProvider(store *ModelStore) *PrincipalProvider {
	return &PrincipalProvider{store: store}
}

// ResolveManagePrincipal 实现 types.IManageRoleProvider。
// 它只在 Manage Casdoor callback/refresh 签发 Token 前建立或读取主体，
// 普通 Manage 请求只读取 Token 中的 RoleCode 并由 Authorizer 查询权限。
func (own *PrincipalProvider) ResolveManagePrincipal(
	ctx context.Context,
	request servertype.ManagePrincipalRequest,
) (servertype.ManagePrincipal, error) {
	identity, err := own.validateRequest(request)
	if err != nil {
		return servertype.ManagePrincipal{}, err
	}
	principal, err := own.resolvePrincipal(ctx, identity, request.Source)
	if err != nil {
		return servertype.ManagePrincipal{}, err
	}
	if err := own.validatePrincipal(principal, identity); err != nil {
		return servertype.ManagePrincipal{}, err
	}
	roles, err := own.resolveRoleRefs(ctx, principal.Code)
	if err != nil {
		return servertype.ManagePrincipal{}, err
	}
	return servertype.ManagePrincipal{Roles: roles}, nil
}

// validateRequest 只接受 Core 已验证的 Casdoor Manage callback/refresh 身份。
func (own *PrincipalProvider) validateRequest(
	request servertype.ManagePrincipalRequest,
) (servertype.AuthIdentity, error) {
	if own == nil || own.store == nil {
		return servertype.AuthIdentity{}, errors.New("manage principal provider is unavailable")
	}
	identity := request.Identity
	identity.UID = strings.TrimSpace(identity.UID)
	identity.Username = strings.TrimSpace(identity.Username)
	identity.ProviderSubject = strings.TrimSpace(identity.ProviderSubject)
	if identity.AuthType != servertype.AuthTypeManage || identity.Provider != servertype.AuthProviderCasdoor ||
		identity.UID == "" || identity.ProviderSubject == "" {
		return servertype.AuthIdentity{}, errors.New("trusted Casdoor Manage identity is required")
	}
	if request.Source != servertype.AuthSourceCallback && request.Source != servertype.AuthSourceRefresh {
		return servertype.AuthIdentity{}, errors.New("unsupported manage principal source")
	}
	return identity, nil
}

// resolvePrincipal 查询已有主体；只有 callback 可以创建首次出现的管理员。
func (own *PrincipalProvider) resolvePrincipal(
	ctx context.Context,
	identity servertype.AuthIdentity,
	source servertype.AuthSource,
) (*smodels.ManagePrincipalModel, error) {
	principal, err := own.store.FindPrincipal(ctx, identity.UID)
	if err != nil {
		return nil, err
	}
	if principal != nil {
		return principal, nil
	}
	if source != servertype.AuthSourceCallback {
		return nil, errors.New("manage principal does not exist")
	}
	return own.createPrincipal(ctx, identity)
}

// createPrincipal 先竞争数据库唯一的首管理员槽位，冲突后改建 viewer。
// 同一 UID 的并发 callback 若已由另一请求建档，则直接复用该结果。
func (own *PrincipalProvider) createPrincipal(
	ctx context.Context,
	identity servertype.AuthIdentity,
) (*smodels.ManagePrincipalModel, error) {
	for attempt := 0; attempt < 3; attempt++ {
		principal := newManagePrincipal(identity)
		bootstrap := attempt == 0
		roleCode := servertype.ManageRoleViewer
		if bootstrap {
			roleCode = servertype.ManageRoleSystemAdmin
		}
		err := own.store.CreatePrincipalWithRole(ctx, principal, roleCode, bootstrap)
		if err == nil {
			return principal, nil
		}
		if servertype.ResolvePublicError(err).Kind != servertype.ErrorKindConflict {
			return nil, err
		}
		stored, findErr := own.store.FindPrincipal(ctx, identity.UID)
		if findErr != nil {
			return nil, findErr
		}
		if stored != nil {
			return stored, nil
		}
	}
	return nil, errors.New("manage principal bootstrap conflict was not resolved")
}

// validatePrincipal 拒绝停用主体或 Casdoor provider/subject 错配。
func (*PrincipalProvider) validatePrincipal(
	principal *smodels.ManagePrincipalModel,
	identity servertype.AuthIdentity,
) error {
	if principal == nil {
		return errors.New("manage principal does not exist")
	}
	if !principal.Enabled || principal.Provider != identity.Provider ||
		principal.ProviderSubject != identity.ProviderSubject {
		return errors.New("manage principal identity is disabled or mismatched")
	}
	return nil
}

// resolveRoleRefs 只返回稳定 RoleCode，不把 path/command 权限写入 Token。
func (own *PrincipalProvider) resolveRoleRefs(
	ctx context.Context,
	principalCode string,
) ([]servertype.ManageRoleRef, error) {
	relations, err := own.store.ListPrincipalRoles(ctx, principalCode)
	if err != nil {
		return nil, err
	}
	roles := make([]servertype.ManageRoleRef, 0, len(relations))
	for _, relation := range relations {
		if relation != nil {
			roles = append(roles, servertype.ManageRoleRef{Code: relation.RoleCode})
		}
	}
	roles, err = servertype.NormalizeManageRoleRefs(roles)
	if err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return nil, errors.New("manage principal has no roles")
	}
	return roles, nil
}

func newManagePrincipal(identity servertype.AuthIdentity) *smodels.ManagePrincipalModel {
	principal := smodels.NewManagePrincipalModel()
	principal.Code = identity.UID
	principal.Username = identity.Username
	principal.Provider = identity.Provider
	principal.ProviderSubject = identity.ProviderSubject
	principal.Enabled = true
	return principal
}
