// 本文件为框架缓存和认证装配专用瞬时通知桥，不接管应用 MQ 主题。
package router

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/digitalwayhk/core/internal/controlnotify"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type internalNotificationBridge struct {
	cfg                                       config.MQConfig
	local                                     *event.ServiceEventBridge
	service, kind, subject, eventType, origin string
	ctx                                       context.Context
	cancel                                    context.CancelFunc
	done                                      chan struct{}
	mu                                        sync.RWMutex
	transport                                 controlnotify.Transport
	epoch                                     uint64
	ready, closed                             bool
	metrics                                   internalNotificationMetrics
}

func newInternalNotificationBridge(ctx context.Context, cfg config.MQConfig, local *event.ServiceEventBridge, service, kind string) (*internalNotificationBridge, error) {
	if local == nil {
		return nil, errors.New("internal notification requires local event bridge")
	}
	t, err := controlnotify.Open(ctx, cfg, service, kind)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	b := &internalNotificationBridge{cfg: cfg, local: local, service: service, kind: kind, origin: rand.Text(),
		ctx: lifetime, cancel: cancel, done: make(chan struct{}), transport: t, epoch: 1, ready: true}
	b.subject = service + ".routecache.invalidate"
	b.eventType = "routecache.invalidate." + service
	if kind == "identity" {
		b.subject = service + ".auth.casdoor.identity.changed"
		b.eventType = types.CasdoorIdentityChangedEventType
	}
	go b.run()
	return b, nil
}

func (b *internalNotificationBridge) Subscribe(eventType string, handler event.Handler) (func(), error) {
	return b.local.Subscribe(eventType, handler)
}

func (b *internalNotificationBridge) SubscribeExternal(ctx context.Context, subject string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if subject != b.subject {
		return nil, errors.New("subject is not registered for internal notification")
	}
	if _, ready := b.NotificationState(); !ready {
		return nil, controlnotify.ErrUnavailable
	}
	// 连接归组合根所有，Manager 退订本地 handler 后不再收到业务回调。
	// 短时构造/恢复 context 不能取消整个 ServiceContext 的通知连接。
	return func() {}, nil
}

func (b *internalNotificationBridge) NotificationState() (uint64, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.epoch, b.ready && !b.closed
}

func (sc *ServiceContext) buildInternalNotificationBridge(kind string) (*internalNotificationBridge, error) {
	if sc.MQManager == nil {
		return nil, errors.New("shared internal notification requires MQ event-stream")
	}
	// 自定义同名工厂可能并非配置中的原生 Broker，不能绕过实际能力偷偷另建连接。
	switch sc.MQManager.Current().(type) {
	case *mq.RedisStreamProvider:
		if sc.Config.MQ.Provider != "redis-stream" {
			return nil, errors.New("internal notification provider configuration mismatch")
		}
	case *mq.NATSJetStreamProvider:
		if sc.Config.MQ.Provider != "nats-jetstream" {
			return nil, errors.New("internal notification provider configuration mismatch")
		}
	default:
		return nil, errors.New("custom MQ provider does not support framework internal notifications")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return newInternalNotificationBridge(ctx, sc.Config.MQ, sc.ServiceEventBridge, sc.Service.Name, kind)
}

func (b *internalNotificationBridge) Publish(ctx context.Context, request event.PublishRequest) (result error) {
	if !request.External {
		return b.local.Publish(ctx, request)
	}
	// 限制整轮外发及本地交付，而非仅限制 Broker 写入。
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	defer func() {
		if result != nil {
			b.metrics.publishFailed.Add(1)
		} else {
			b.metrics.published.Add(1)
		}
	}()
	if request.Subject != b.subject || request.Envelope == nil {
		return errors.New("subject is not registered for internal notification")
	}
	env := *request.Envelope
	if request.BuildData != nil {
		data, err := request.BuildData()
		if err != nil {
			return err
		}
		env.Data = data
	}
	if err := b.validateEnvelope(&env); err != nil {
		return err
	}
	data, err := json.Marshal(internalNotificationFrame{Version: 1, Origin: b.origin, Envelope: &env})
	if err != nil {
		return err
	}
	b.mu.RLock()
	t, ready := b.transport, b.ready && !b.closed
	b.mu.RUnlock()
	if !ready {
		return controlnotify.ErrUnavailable
	}
	if err := t.Publish(ctx, data); err != nil {
		b.invalidate(t)
		return err
	}
	request.External = false
	request.Envelope = &env
	request.BuildData = nil
	if err := b.local.Publish(ctx, request); err != nil {
		b.invalidate(t)
		return err
	}
	return nil
}

func (b *internalNotificationBridge) validateEnvelope(env *event.Envelope) error {
	if env == nil || env.Source != b.service || env.Type != b.eventType {
		return errors.New("invalid internal notification envelope")
	}
	if b.kind == "identity" {
		var payload types.CasdoorEvent
		if json.Unmarshal(env.Data, &payload) != nil || payload.ServiceName != b.service || payload.Provider != types.AuthProviderCasdoor || payload.ProviderSubject == "" {
			return errors.New("invalid identity notification")
		}
	} else {
		var payload struct {
			Service string `json:"service"`
			Route   string `json:"route"`
		}
		if json.Unmarshal(env.Data, &payload) != nil || payload.Service != b.service || payload.Route == "" {
			return errors.New("invalid cache notification")
		}
	}
	return nil
}

func (b *internalNotificationBridge) invalidate(t controlnotify.Transport) {
	b.mu.Lock()
	changed := b.transport == t && b.ready
	if changed {
		b.ready = false
		b.epoch++
		b.metrics.gaps.Add(1)
	}
	b.mu.Unlock()
	_ = t.Close()
	if changed {
		logx.Errorw("internal_notification_unavailable", logx.Field("service", b.service), logx.Field("kind", b.kind))
	}
}

func (b *internalNotificationBridge) run() {
	defer close(b.done)
	for {
		b.mu.RLock()
		t := b.transport
		b.mu.RUnlock()
		data, err := t.Receive(b.ctx)
		if err == nil {
			var frame internalNotificationFrame
			if json.Unmarshal(data, &frame) != nil || frame.Version != 1 || frame.Origin == "" {
				err = errors.New("invalid notification frame")
			} else {
				err = b.validateEnvelope(frame.Envelope)
			}
			if err == nil && frame.Origin != b.origin {
				ctx, cancel := context.WithTimeout(b.ctx, 3*time.Second)
				err = b.local.Publish(ctx, event.PublishRequest{Class: event.ControlDelivery, Envelope: frame.Envelope})
				cancel()
			}
			if err == nil {
				continue
			}
		}
		b.invalidate(t)
		for {
			timer := time.NewTimer(time.Second)
			select {
			case <-b.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			ctx, cancel := context.WithTimeout(b.ctx, 3*time.Second)
			replacement, err := controlnotify.Open(ctx, b.cfg, b.service, b.kind)
			cancel()
			if err != nil {
				continue
			}
			b.mu.Lock()
			if b.closed {
				b.mu.Unlock()
				_ = replacement.Close()
				return
			}
			b.transport = replacement
			b.epoch++
			b.ready = true
			b.mu.Unlock()
			logx.Infow("internal_notification_reconnected", logx.Field("service", b.service), logx.Field("kind", b.kind))
			break
		}
	}
}

func (b *internalNotificationBridge) Close() error {
	b.mu.Lock()
	b.closed = true
	b.ready = false
	t := b.transport
	b.mu.Unlock()
	b.cancel()
	err := t.Close()
	<-b.done
	return err
}
