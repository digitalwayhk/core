// 本文件只管理发布器内存中的失败key退避资格，不修改Outbox原件、顺序或确认事实。
package event

import "time"

// advanceOutboxRetry到期才自动释放屏障；Notify在同一退避窗口最多提前释放一次，不能无限重置重试预算。
// 清屏障只允许按earliest-first重新尝试最早未确认原件，不是确认成功或放行同key后继。
func (p *outboxPublisher) advanceOutboxRetry(now time.Time, explicit bool) {
	if !p.retryAt.IsZero() && !now.Before(p.retryAt) {
		clear(p.blocked)
		p.retryAt = time.Time{}
		p.retryExpedited = false
	} else if explicit && len(p.blocked) > 0 && !p.retryExpedited {
		clear(p.blocked)
		p.retryExpedited = true
	}
	p.blockedKeys.Store(int64(len(p.blocked)))
}
