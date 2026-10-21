// 本文件定义Provider中立的持续租约与会话引用存储合同。
package ownership

import (
	"context"
	"time"
)

// Store 是已有MQ Provider的可选归属能力，不由业务实现Redis细节。
type Store interface {
	Acquire(context.Context, Credential, time.Duration) (Credential, error)
	Renew(context.Context, Credential, time.Duration) error
	Ready(context.Context, Credential) error
	Resolve(context.Context, string, string) (Credential, error)
	Validate(context.Context, Credential, bool) error
	Release(context.Context, Credential) error
	OpenGateway(context.Context, string, time.Duration) error
	ReplaceGateway(context.Context, GatewaySnapshot) error
	CloseGateway(context.Context, string) error
	References(context.Context, string, string) (ReferenceSnapshot, error)
}
