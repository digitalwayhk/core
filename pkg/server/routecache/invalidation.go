package routecache

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/event"
)

type InvalidationBridge interface {
	Subscribe(eventType string, handler event.Handler) (func(), error)
	SubscribeExternal(ctx context.Context, subject string) (func(), error)
	Publish(ctx context.Context, request event.PublishRequest) error
}

type invalidationEvent struct {
	Service    string `json:"service"`
	Route      string `json:"route"`
	Key        string `json:"key,omitempty"`
	Generation uint64 `json:"generation"`
}

func invalidationEventType(service string) string {
	return "routecache.invalidate." + service
}

func invalidationSubject(service string) string {
	return service + ".routecache.invalidate"
}

func (m *Manager) subscribeInvalidation(ctx context.Context) error {
	if m.events == nil {
		return errors.New("route cache shared mode requires an EventBridge invalidation adapter")
	}
	localCancel, err := m.events.Subscribe(invalidationEventType(m.service), m.handleInvalidation)
	if err != nil {
		return err
	}
	externalCancel, err := m.events.SubscribeExternal(ctx, invalidationSubject(m.service))
	if err != nil {
		localCancel()
		return err
	}
	m.subscriptionMu.Lock()
	m.localCancel = localCancel
	m.externalCancel = externalCancel
	m.subscriptionMu.Unlock()
	m.invalidationReady.Store(true)
	return nil
}

func (m *Manager) resubscribeExternal(ctx context.Context) error {
	if m.events == nil {
		return errors.New("route cache invalidation bridge is unavailable")
	}
	cancel, err := m.events.SubscribeExternal(ctx, invalidationSubject(m.service))
	if err != nil {
		return err
	}
	m.subscriptionMu.Lock()
	previous := m.externalCancel
	m.externalCancel = cancel
	m.subscriptionMu.Unlock()
	if previous != nil {
		previous()
	}
	m.invalidationReady.Store(true)
	return nil
}

func (m *Manager) publishInvalidation(route, key string, generation uint64) error {
	payload := invalidationEvent{
		Service:    m.service,
		Route:      route,
		Key:        key,
		Generation: generation,
	}
	envelope := event.NewEnvelope(m.service, invalidationEventType(m.service), nil)
	envelope.Subject = route
	envelope.ShardKey = route + ":" + key
	return m.events.Publish(context.Background(), event.PublishRequest{
		Class:    event.ControlDelivery,
		External: true,
		Subject:  invalidationSubject(m.service),
		Envelope: envelope,
		BuildData: func() ([]byte, error) {
			return json.Marshal(payload)
		},
	})
}

func (m *Manager) handleInvalidation(envelope *event.Envelope) {
	if envelope == nil {
		return
	}
	payload := invalidationEvent{}
	if json.Unmarshal(envelope.Data, &payload) != nil || payload.Service != m.service || payload.Route == "" {
		return
	}
	if payload.Key == "" {
		m.routesMu.Lock()
		policy, ok := m.routes[payload.Route]
		if ok && payload.Generation > policy.generation {
			policy.generation = payload.Generation
			m.routes[payload.Route] = policy
		}
		m.routesMu.Unlock()
		_ = m.clearRouteLocal(payload.Route)
		return
	}
	if !strings.HasPrefix(payload.Key, m.routePrefix(payload.Route)) {
		return
	}
	if m.l2 != nil {
		_ = m.l2.Delete(payload.Key)
	}
	if m.l1 != nil {
		m.l1.Delete(payload.Key)
	}
}

func (m *Manager) cancelInvalidationSubscriptions() {
	m.subscriptionMu.Lock()
	localCancel := m.localCancel
	externalCancel := m.externalCancel
	m.localCancel = nil
	m.externalCancel = nil
	m.subscriptionMu.Unlock()
	if externalCancel != nil {
		externalCancel()
	}
	if localCancel != nil {
		localCancel()
	}
}
