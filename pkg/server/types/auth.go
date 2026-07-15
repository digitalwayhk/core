package types

import (
	"context"
	"time"
)

// IClaimsMutator 允许认证钩子向即将颁发的 Access Token 注入自定义 Claims。
type IClaimsMutator interface {
	AddData(key, value string)
}

// AuthType 标识 Token 可访问的路由类型。
type AuthType string

const (
	AuthTypeUser         AuthType = "auth"
	AuthTypeManage       AuthType = "manage"
	AuthTypeServerManage AuthType = "servermanage"
)

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
}

// IAuthHookProvider 由服务可选实现，用于在签名前拒绝颁发或注入业务 Claims。
type IAuthHookProvider interface {
	OnAuth(ctx context.Context, args *AuthHookArgs) error
}
