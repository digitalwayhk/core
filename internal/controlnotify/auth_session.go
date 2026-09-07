// 本文件通过内部上下文区分长连接连续性检查与普通 HTTP 权威授权，不扩张公共认证 API。
package controlnotify

import (
	"context"
	"sync"
)

type authSessionKey struct{}

// AuthSessionState 保存一次 WebSocket 登录捕获的通知代次，可供登录、订阅和周期检查共用。
type AuthSessionState struct {
	mu       sync.Mutex
	epoch    uint64
	captured bool
}

// Accept 只接受首次健康连接或同一健康代次；跨断线恢复不能重新认可旧会话。
func (s *AuthSessionState) Accept(epoch uint64, ready bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ready || s.captured && s.epoch != epoch {
		return false
	}
	s.epoch = epoch
	s.captured = true
	return true
}

// Active 报告授权管理器是否为此会话启用了共享通知连续性保护。
func (s *AuthSessionState) Active() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.captured
}

// WithAuthSession 仅供 Core WebSocket 会话层标记需要连续性保护的权威检查。
func WithAuthSession(ctx context.Context, state *AuthSessionState) context.Context {
	return context.WithValue(ctx, authSessionKey{}, state)
}

// AuthSession 返回内部会话标记；普通 HTTP 请求不含该值。
func AuthSession(ctx context.Context) *AuthSessionState {
	state, _ := ctx.Value(authSessionKey{}).(*AuthSessionState)
	return state
}
