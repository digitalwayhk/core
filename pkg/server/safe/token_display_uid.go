// 本文件提供显式展示 UID；认证 UID 始终保留在 Claims.Uid 和验签后的身份中。
package safe

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/types"
)

const authIdentityUIDClaim = "auth_identity_uid"

var _ types.ITokenUIDMutator = (*Claims)(nil)

// SetTokenUID 只设置普通用户 Token 的展示 uid，不改变认证、撤销或秘密 Claim 的身份。
// 非法值会阻止签发，即使调用方忽略本方法返回的错误。
func (own *Claims) SetTokenUID(uid string) error {
	if own == nil {
		return errors.New("Token Claims 不能为空")
	}
	if !validTokenUID(uid) {
		own.tokenUIDErr = errors.New("Token 展示 UID 无效")
		return own.tokenUIDErr
	}
	if own.tokenUIDErr != nil {
		return own.tokenUIDErr
	}
	own.tokenUID = uid
	return nil
}

func validTokenUID(uid string) bool {
	return uid != "" && len(uid) <= 128 && strings.TrimSpace(uid) == uid && !strings.ContainsAny(uid, "\x00\r\n\t")
}

func (own *Claims) wireUID() string {
	if own.tokenUID != "" {
		return own.tokenUID
	}
	return own.Uid
}

func addTokenIdentityUID(claims map[string]interface{}, own *Claims) {
	if own.wireUID() != own.Uid {
		claims[authIdentityUIDClaim] = own.Uid
	}
}

func tokenAuthenticationUID(claims map[string]interface{}, authType types.AuthType, uid string) (string, error) {
	raw, exists := claims[authIdentityUIDClaim]
	if !exists {
		return uid, nil
	}
	original, ok := raw.(string)
	if authType != types.AuthTypeUser || !ok || !validTokenUID(original) || !validTokenUID(uid) {
		return "", errors.New("auth_identity_uid无效")
	}
	return original, nil
}
