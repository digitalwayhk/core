package types

import (
	"context"
	"time"
)

// IClaimsMutator 允许认证钩子向即将颁发的 Access Token 注入自定义 Claims。
type IClaimsMutator interface {
	AddData(key, value string)
}

// ISecretClaimsMutator 允许认证钩子注入只能在服务端验签后读取的秘密 Claim。
// 独立于 IClaimsMutator 以保持既有消费方实现的源码兼容。
type ISecretClaimsMutator interface {
	AddSecretData(key, value string) error
}

// AuthType 标识 Token 可访问的路由类型。
type AuthType string

const (
	AuthTypeUser         AuthType = "auth"
	AuthTypeManage       AuthType = "manage"
	AuthTypeServerManage AuthType = "servermanage"
)

const AuthProviderCasdoor = "casdoor"

// AuthSource 标识本次颁发请求的发起入口。
type AuthSource string

const (
	AuthSourceCallback  AuthSource = "callback"
	AuthSourceRefresh   AuthSource = "refresh"
	AuthSourceTestToken AuthSource = "testtoken"
)

// AuthHookArgs 是框架在签名 Token 前传给认证钩子的完整上下文。
// UID 必须非空；时间字段必须与最终 Token 使用的 iat/exp 一致。
type AuthHookArgs struct {
	UID                  string
	Username             string
	AuthType             AuthType
	Source               AuthSource
	IssuedAt             time.Time
	AccessExpireSeconds  int64
	RefreshExpireSeconds int64
	AccessExpiresAt      time.Time
	RefreshExpiresAt     time.Time
	Extra                interface{}
	Claims               IClaimsMutator
	SecretClaims         ISecretClaimsMutator
	Identity             AuthIdentity
}

// IAuthHookProvider 由服务可选实现，用于在签名前拒绝颁发或注入业务 Claims。
type IAuthHookProvider interface {
	OnAuth(ctx context.Context, args *AuthHookArgs) error
}

// AuthIdentity 是签名验证后可在认证边界间传递的规范身份。
// Provider 为空表示框架内建或测试 Token，不执行 Casdoor 世代校验。
type AuthIdentity struct {
	UID              string
	Username         string
	AuthType         AuthType
	Provider         string
	ProviderSubject  string
	Generation       uint64
	AuthorityService string
	IssuedAt         time.Time
	ExpiresAt        time.Time
}

// AuthRequestArgs 是业务 Router 执行前传给服务授权 Hook 的不可变快照。
// Claims 必须通过 CloneAuthClaims 构造，不能与 JWT context 共享可变 map/slice。
type AuthRequestArgs struct {
	Identity     AuthIdentity
	ServiceName  string
	Path         string
	Method       string
	PathType     ApiType
	ClientIP     string
	TraceID      string
	Claims       map[string]interface{}
	SecretClaims map[string]string
}

// CasdoorEvent 是框架验证、规范化并持久化后的身份控制事件。
// 它不得包含 Token、Secret、Header 或原始 Webhook Payload。
type CasdoorEvent struct {
	ID              string
	ServiceName     string
	AuthType        AuthType
	Provider        string
	ProviderSubject string
	UID             string
	EventType       string
	EventOrder      int64
	Generation      uint64
	Blocked         bool
	OccurredAt      time.Time
}

// IAuthRequestHookProvider 由服务可选实现，用于在已认证请求执行 Router 前施加业务授权。
type IAuthRequestHookProvider interface {
	OnAuthRequest(ctx context.Context, args AuthRequestArgs) error
}

// ICasdoorEventHookProvider 由服务可选实现，用于异步处理已完成框架撤销的标准事件。
type ICasdoorEventHookProvider interface {
	OnCasdoorEvent(ctx context.Context, event CasdoorEvent) error
}

// CloneAuthClaims 递归复制 JWT Claims 中常见的 map/slice，避免 Hook 修改框架持有值。
func CloneAuthClaims(source map[string]interface{}) map[string]interface{} {
	if source == nil {
		return nil
	}
	result := make(map[string]interface{}, len(source))
	for key, value := range source {
		result[key] = cloneAuthClaimValue(value)
	}
	return result
}

// CloneSecretClaims 复制已验签并解密的服务端秘密 Claim。
func CloneSecretClaims(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func cloneAuthClaimValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		return CloneAuthClaims(typed)
	case []interface{}:
		result := make([]interface{}, len(typed))
		for i, item := range typed {
			result[i] = cloneAuthClaimValue(item)
		}
		return result
	case []string:
		return append([]string(nil), typed...)
	case map[string]string:
		result := make(map[string]string, len(typed))
		for key, item := range typed {
			result[key] = item
		}
		return result
	default:
		return typed
	}
}
