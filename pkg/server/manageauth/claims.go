// 本文件从已验签 JWT Claims 中严格恢复规范化 Manage RoleCode。
package manageauth

import (
	"encoding/json"
	"errors"
	"fmt"

	servertype "github.com/digitalwayhk/core/pkg/server/types"
)

const maxManageRolesClaimBytes = 4096

// ManageRolesFromClaims 从已验签 Access Token Claims 恢复标准 RoleCode 列表。
// 只有规范 JSON 字符串被接受，避免不同 JWT 解码表示造成授权歧义。
func ManageRolesFromClaims(claims map[string]interface{}) ([]servertype.ManageRoleRef, error) {
	if claims == nil {
		return nil, errors.New("manage roles claim is missing")
	}
	raw, exists := claims[servertype.ManageRolesClaim]
	if !exists {
		return nil, errors.New("manage roles claim is missing")
	}
	encoded, ok := raw.(string)
	if !ok || encoded == "" || len(encoded) > maxManageRolesClaimBytes {
		return nil, errors.New("manage roles claim is invalid")
	}

	var codes []string
	if err := json.Unmarshal([]byte(encoded), &codes); err != nil {
		return nil, fmt.Errorf("manage roles claim is invalid: %w", err)
	}
	refs := make([]servertype.ManageRoleRef, len(codes))
	for i, code := range codes {
		refs[i] = servertype.ManageRoleRef{Code: code}
	}
	return servertype.NormalizeManageRoleRefs(refs)
}
