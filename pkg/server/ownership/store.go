// 本文件定义Provider中立的持续租约与会话引用存储合同。
package ownership

import (
	"context"
	"time"
)

// Store 是已有MQ Provider的可选归属能力，不由业务实现Redis细节。
type Store interface {
	// Acquire 取得一个尚无持有者的声明键，返回正epoch和独立会话凭据。
	Acquire(context.Context, Credential, time.Duration) (Credential, error)
	// Renew 仅为完整匹配的现持有者续租，未知结果不能当作成功。
	Renew(context.Context, Credential, time.Duration) error
	// Ready 在应用fence与引用恢复完成后公布该凭据。
	Ready(context.Context, Credential) error
	// Resolve 只解析当前Ready持有者，不返回健康哈希候选。
	Resolve(context.Context, string, string) (Credential, error)
	// Validate 验证凭据仍有效，布尔值表示是否同时要求Ready。
	Validate(context.Context, Credential, bool) error
	// Release 只释放完整匹配的租约，旧凭据不能释放新owner。
	Release(context.Context, Credential) error
	// OpenGateway 为框架Gateway启动一个有界恢复会话。
	OpenGateway(context.Context, string, time.Duration) error
	// ReplaceGateway 按正revision原子替换该Gateway全量引用，拒绝迟到快照。
	ReplaceGateway(context.Context, GatewaySnapshot) error
	// CloseGateway 关闭该Gateway恢复会话，不移除其他Gateway引用。
	CloseGateway(context.Context, string) error
	// References 返回单键收敛的活跃引用及正revision，未知不能补零。
	References(context.Context, string, string) (ReferenceSnapshot, error)
}
