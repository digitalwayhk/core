// 本文件定义可选外部角色 Provider、Core 授权器和稳定 RoleCode 的公共契约。
package types

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	// ManageRolesClaim 是 Manage Access Token 中保存 RoleCode JSON 数组的保留 Claim。
	ManageRolesClaim = "manage_roles"

	// ManageRoleSystemAdmin 是动态允许全部 Manage command 的内置角色。
	ManageRoleSystemAdmin = "core.system_admin"
	// ManageRoleViewer 是动态允许 view/search Manage command 的内置角色。
	ManageRoleViewer = "core.viewer"

	// ManageRolePolicyGrantAll 表示动态允许全部 Manage command。
	ManageRolePolicyGrantAll = "grant_all"
	// ManageRolePolicyReadOnly 表示动态只允许 view/search。
	ManageRolePolicyReadOnly = "read_only"
	// ManageRolePolicyExplicit 表示从权限明细表精确查询。
	ManageRolePolicyExplicit = "explicit"

	// MaxManageRoleCodes 限制单个身份可写入 Token 的角色数量。
	MaxManageRoleCodes = 32
	// MaxManageRoleCodeLength 与持久化 RoleCode 字段长度保持一致。
	MaxManageRoleCodeLength = 128
)

var manageRoleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)

// ManageRoleRef 是跨 Token 和管理员主体角色关系使用的稳定角色引用。
// Code 不得使用数据库 ID，也不得携带权限明细。
type ManageRoleRef struct {
	Code string `json:"code"`
}

// ManagePrincipalRequest 是 Core 请求角色 Provider 解析 Manage 角色的可信身份快照。
type ManagePrincipalRequest struct {
	Identity     AuthIdentity
	Source       AuthSource
	DefaultRoles []ManageRoleRef
}

// ManagePrincipal 是角色 Provider 返回给 Core 的标准 Manage 身份授权信息。
type ManagePrincipal struct {
	Roles []ManageRoleRef
}

// IManageRoleProvider 允许服务在角色权威位于外部 IAM 时覆盖 Core 默认主体映射。
// Provider 只返回角色，不返回 path/command 权限明细，也不接收 Core 存储 action。
type IManageRoleProvider interface {
	ResolveManagePrincipal(context.Context, ManagePrincipalRequest) (ManagePrincipal, error)
}

// ManageAuthorizationRequest 标识一条需要精确匹配的 Manage Router 权限。
type ManageAuthorizationRequest struct {
	Service string
	Path    string
	Command string
}

// IManageAuthorizer 是 REST 认证边界调用的 Manage 授权器契约。
type IManageAuthorizer interface {
	Authorize(context.Context, []ManageRoleRef, ManageAuthorizationRequest) error
}

// NormalizeManageRoleRefs 校验、去重并按 Code 排序角色引用。
func NormalizeManageRoleRefs(values []ManageRoleRef) ([]ManageRoleRef, error) {
	if len(values) > MaxManageRoleCodes {
		return nil, fmt.Errorf("manage role count exceeds %d", MaxManageRoleCodes)
	}

	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		code := strings.TrimSpace(value.Code)
		if err := ValidateManageRoleCode(code); err != nil {
			return nil, err
		}
		seen[code] = struct{}{}
	}

	result := make([]ManageRoleRef, 0, len(seen))
	for code := range seen {
		result = append(result, ManageRoleRef{Code: code})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Code < result[j].Code
	})
	return result, nil
}

// ValidateManageRoleCode 校验公开稳定的 RoleCode。
func ValidateManageRoleCode(code string) error {
	if code == "" {
		return fmt.Errorf("manage role code is required")
	}
	if len(code) > MaxManageRoleCodeLength {
		return fmt.Errorf("manage role code exceeds %d bytes", MaxManageRoleCodeLength)
	}
	if !manageRoleCodePattern.MatchString(code) {
		return fmt.Errorf("manage role code is invalid")
	}
	return nil
}
