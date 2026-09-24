// Package manageauth 实现 Core 服务端 Manage RBAC 的角色解析、精确匹配与安全失败边界。
package manageauth

import (
	"context"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
)

// ErrManagePermissionDenied 是服务端精确权限拒绝的可识别 cause。
var ErrManagePermissionDenied = errors.New("manage permission denied")

// Request 是 Manage 授权目标的兼容别名。
type Request = servertype.ManageAuthorizationRequest

// Store 查询启用的自定义角色及其精确权限。
type Store interface {
	FindRole(ctx context.Context, code string) (*smodels.ManageRoleModel, error)
	HasPermission(ctx context.Context, roleCode, service, path, command string) (bool, error)
}

// Authorizer 动态计算内置策略并查询自定义角色权限。
type Authorizer struct {
	store Store
}

// NewAuthorizer 使用给定权限存储创建授权器。
func NewAuthorizer(store Store) *Authorizer {
	return &Authorizer{store: store}
}

// Authorize 动态计算内置策略，并以并集方式精确匹配自定义角色权限。
// 自定义权限不缓存，因此变更会在下一次请求生效。
func (own *Authorizer) Authorize(ctx context.Context, roles []servertype.ManageRoleRef, request Request) error {
	request.Service = strings.TrimSpace(request.Service)
	request.Path = strings.TrimSpace(request.Path)
	request.Command = strings.TrimSpace(request.Command)
	if request.Service == "" || request.Command == "" ||
		request.Service != strings.ToLower(request.Service) ||
		request.Command != strings.ToLower(request.Command) ||
		!strings.HasPrefix(request.Path, "/api/manage/") {
		return internalAuthorizationError(errors.New("invalid manage authorization target"))
	}

	normalized, err := servertype.NormalizeManageRoleRefs(roles)
	if err != nil {
		return internalAuthorizationError(err)
	}
	for _, role := range normalized {
		switch role.Code {
		case servertype.ManageRoleSystemAdmin:
			return nil
		case servertype.ManageRoleViewer:
			if request.Command == "view" || request.Command == "search" {
				return nil
			}
			continue
		}

		if own == nil || own.store == nil {
			return internalAuthorizationError(errors.New("manage role store is unavailable"))
		}
		stored, err := own.store.FindRole(ctx, role.Code)
		if err != nil {
			return internalAuthorizationError(err)
		}
		if stored == nil || !stored.Enabled {
			continue
		}
		if stored.Policy != servertype.ManageRolePolicyExplicit || stored.Code != role.Code {
			return internalAuthorizationError(errors.New("invalid custom manage role policy"))
		}
		allowed, err := own.store.HasPermission(
			ctx,
			role.Code,
			request.Service,
			request.Path,
			request.Command,
		)
		if err != nil {
			return internalAuthorizationError(err)
		}
		if allowed {
			return nil
		}
	}
	return servertype.NewPublicError(
		servertype.ErrorKindForbidden,
		servertype.PublicCodeForbidden,
		"permission denied",
		ErrManagePermissionDenied,
	)
}

func internalAuthorizationError(cause error) error {
	return servertype.NewPublicError(
		servertype.ErrorKindInternal,
		servertype.PublicCodeInternal,
		"internal server error",
		cause,
	)
}
