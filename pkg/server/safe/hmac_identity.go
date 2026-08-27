// Package safe 在本文件中负责校验 HMAC Provider 返回的身份并构造框架可信访问上下文。
package safe

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
)

// BuildHMACAccessIdentity 校验服务返回的 HMAC 身份，并构造 REST 与 WebSocket 共用的可信访问身份。
func BuildHMACAccessIdentity(
	result *types.HMACAuthResult,
	accessKey string,
	expectedAuthType types.AuthType,
	now time.Time,
	maxLifetime time.Duration,
) (*AccessTokenIdentity, error) {
	if result == nil || now.IsZero() || maxLifetime <= 0 {
		return nil, errors.New("HMAC identity parameters are invalid")
	}
	identity := result.Identity
	if strings.TrimSpace(identity.UID) == "" || identity.AuthType != expectedAuthType {
		return nil, errors.New("HMAC identity user or auth type is invalid")
	}
	if strings.TrimSpace(identity.Provider) == "" || identity.Provider == types.AuthProviderCasdoor {
		return nil, errors.New("HMAC identity provider is invalid")
	}
	if strings.TrimSpace(identity.ProviderSubject) == "" || identity.ProviderSubject == accessKey {
		return nil, errors.New("HMAC identity provider subject must be a stable credential ID")
	}
	if identity.Generation != 0 || strings.TrimSpace(identity.AuthorityService) != "" {
		return nil, errors.New("HMAC identity cannot carry Casdoor authority state")
	}
	if err := ValidateHMACAuthClaims(result.Claims); err != nil {
		return nil, err
	}

	expiresAt := now.Add(maxLifetime)
	if !identity.ExpiresAt.IsZero() {
		if !identity.ExpiresAt.After(now) {
			return nil, errors.New("HMAC identity has expired")
		}
		if identity.ExpiresAt.Before(expiresAt) {
			expiresAt = identity.ExpiresAt.UTC()
		}
	}
	identity.IssuedAt = now.UTC()
	identity.ExpiresAt = expiresAt.UTC()

	claims := map[string]interface{}{
		"uid": identity.UID, "uname": identity.Username, "auth_type": string(identity.AuthType),
		"token_use": "access", "iat": identity.IssuedAt.Unix(), "exp": identity.ExpiresAt.Unix(),
		"auth_provider": identity.Provider, "provider_subject": identity.ProviderSubject,
	}
	for key, value := range result.Claims {
		claims[key] = value
	}
	return &AccessTokenIdentity{
		UID: identity.UID, Username: identity.Username, AuthType: identity.AuthType,
		IssuedAt: identity.IssuedAt, ExpiresAt: identity.ExpiresAt, Identity: identity,
		Claims: types.CloneAuthClaims(claims),
	}, nil
}

// ValidateHMACAuthClaims 供消费方在返回 HMACAuthResult 前预检查业务 Claims。
// 保留键为 uid、uname、auth_type、token_use、iat、exp、auth_provider、
// provider_subject、auth_generation、auth_authority_service、args 和 secret_args。
func ValidateHMACAuthClaims(claims map[string]string) error {
	for key := range claims {
		if _, reserved := reservedTokenClaims[key]; reserved {
			return fmt.Errorf("HMAC identity claim %q is reserved", key)
		}
	}
	return nil
}
