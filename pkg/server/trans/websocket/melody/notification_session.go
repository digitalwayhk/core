// 本文件保护已登录但尚未订阅路由的 Casdoor 会话，并由登录代次隔离取消和重登。
package melody

import (
	"context"
	"time"

	"github.com/digitalwayhk/core/internal/controlnotify"
	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/safe"
)

func (s *SessionSubscriptions) startNotificationSessionLocked(manager *authstate.Manager, identity *safe.AccessTokenIdentity, state *controlnotify.AuthSessionState) {
	s.stopNotificationSessionLocked()
	if manager == nil || state == nil || !state.Active() {
		return
	}
	lifetime := s.sessionContext
	if lifetime == nil {
		lifetime = context.Background()
	}
	ctx, cancel := context.WithCancel(lifetime)
	s.notificationMu.Lock()
	owned := s.notificationGen.Add(1)
	s.notificationCancel = cancel
	s.notificationExpired.Store(false)
	s.notificationState = state
	s.notificationWatchdog = time.AfterFunc(5*time.Second, func() { s.expireNotificationSession(owned) })
	s.notificationMu.Unlock()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		defer cancel()
		lastChecked := time.Now()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if time.Since(lastChecked) >= 5*time.Second {
				s.expireNotificationSession(owned)
				return
			}
			checkCtx, stop := context.WithTimeout(ctx, 3*time.Second)
			err := manager.Authorize(controlnotify.WithAuthSession(checkCtx, state), identity.Identity)
			stop()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				s.expireNotificationSession(owned)
				return
			}
			s.notificationMu.Lock()
			current := s.notificationGen.Load() == owned && s.notificationWatchdog != nil
			if current {
				current = s.notificationWatchdog.Reset(5 * time.Second)
			}
			s.notificationMu.Unlock()
			if !current {
				s.expireNotificationSession(owned)
				return
			}
			lastChecked = time.Now()
		}
	}()
}

func (s *SessionSubscriptions) stopNotificationSessionLocked() {
	s.notificationMu.Lock()
	defer s.notificationMu.Unlock()
	s.notificationGen.Add(1)
	if s.notificationCancel != nil {
		s.notificationCancel()
		s.notificationCancel = nil
	}
	if s.notificationWatchdog != nil {
		s.notificationWatchdog.Stop()
		s.notificationWatchdog = nil
	}
	s.notificationState = nil
	s.notificationExpired.Store(false)
}

func (s *SessionSubscriptions) expireNotificationSession(generation uint64) {
	s.notificationMu.Lock()
	if !s.notificationGen.CompareAndSwap(generation, generation+1) {
		s.notificationMu.Unlock()
		return
	}
	s.notificationExpired.Store(true)
	if s.notificationCancel != nil {
		s.notificationCancel()
	}
	if s.notificationWatchdog != nil {
		s.notificationWatchdog.Stop()
		s.notificationWatchdog = nil
	}
	if s.client != nil {
		_ = s.client.Close()
	}
	s.notificationMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notificationGen.Load() == generation+1 {
		s.logoutLocked()
	}
}
