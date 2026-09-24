// Package manageauth implements Core's server-side Manage RBAC boundary.
package manageauth

import (
	"context"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
)

var ErrManagePermissionDenied = errors.New("manage permission denied")

// Request 是 Manage 授权目标的兼容别名。
type Request = servertype.ManageAuthorizationRequest

// Store resolves enabled custom roles and their exact permissions.
type Store interface {
	FindRole(ctx context.Context, code string) (*smodels.ManageRoleModel, error)
	HasPermission(ctx context.Context, roleCode, service, path, command string) (bool, error)
}

type Authorizer struct {
	store Store
}

func NewAuthorizer(store Store) *Authorizer {
	return &Authorizer{store: store}
}

// Authorize evaluates built-in policies dynamically and custom role permissions as a union.
// It deliberately does not cache custom permission rows, so permission changes affect the next request.
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
