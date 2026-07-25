package authstate

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
)

var (
	ErrAuthorityUnavailable = errors.New("认证撤销权威存储不可用")
	ErrIdentityRevoked      = errors.New("认证身份已失效")
	ErrGenerationChanged    = errors.New("认证身份世代已变更")
	ErrInvalidIdentity      = errors.New("认证身份无效")
	ErrInvalidEvent         = errors.New("Casdoor事件无效")
	ErrEventNotFound        = errors.New("Casdoor事件不存在")
)

// IdentityKey 唯一标识某服务某认证域中的外部身份。
type IdentityKey struct {
	Service  string         `json:"service"`
	AuthType types.AuthType `json:"auth_type"`
	Provider string         `json:"provider"`
	Subject  string         `json:"subject"`
}

func (k IdentityKey) validate() error {
	if strings.TrimSpace(k.Service) == "" || strings.TrimSpace(k.Provider) == "" || strings.TrimSpace(k.Subject) == "" {
		return ErrInvalidIdentity
	}
	if k.AuthType != types.AuthTypeUser && k.AuthType != types.AuthTypeManage {
		return ErrInvalidIdentity
	}
	return nil
}

func (k IdentityKey) encoded() string {
	parts := []string{k.Service, string(k.AuthType), k.Provider, k.Subject}
	for index := range parts {
		parts[index] = base64.RawURLEncoding.EncodeToString([]byte(parts[index]))
	}
	return strings.Join(parts, "/")
}

// State 是身份撤销权威中的单调状态。
type State struct {
	Key        IdentityKey `json:"key"`
	Generation uint64      `json:"generation"`
	Blocked    bool        `json:"blocked"`
	EventOrder int64       `json:"event_order"`
	UID        string      `json:"uid,omitempty"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// ApplyResult 区分首次应用、重复/乱序事件及控制通知是否已发布。
type ApplyResult struct {
	Applied          bool
	Generation       uint64
	ControlPublished bool
	State            State
}

// PendingHook 是已完成框架撤销、待重试业务事件 Hook 的标准记录。
type PendingHook struct {
	ID          string             `json:"id"`
	Event       types.CasdoorEvent `json:"event"`
	Ready       bool               `json:"ready"`
	Attempts    int                `json:"attempts"`
	NextAttempt time.Time          `json:"next_attempt"`
}

// Store 是本地 Badger 与共享 Redis 共用的撤销权威契约。
type Store interface {
	Current(context.Context, IdentityKey) (State, error)
	Apply(context.Context, types.CasdoorEvent, time.Duration) (ApplyResult, error)
	ConfirmActive(context.Context, IdentityKey, uint64) (State, error)
	SaveSnapshot(context.Context, State) error
	MarkControlPublished(context.Context, types.CasdoorEvent) error
	SavePendingHook(context.Context, PendingHook) error
	MarkPendingHookReady(context.Context, string) error
	PendingHooks(context.Context, int) ([]PendingHook, error)
	AckHook(context.Context, string) error
	Close() error
}

func eventIdentityKey(event types.CasdoorEvent) IdentityKey {
	return IdentityKey{
		Service:  event.ServiceName,
		AuthType: event.AuthType,
		Provider: event.Provider,
		Subject:  event.ProviderSubject,
	}
}

// identityAuthorityService 解析撤销命名空间：优先使用签名权威服务，否则回退当前服务。
func identityAuthorityService(managerService string, identity types.AuthIdentity) string {
	if authority := strings.TrimSpace(identity.AuthorityService); authority != "" {
		return strings.ToLower(authority)
	}
	return managerService
}

func identityKey(service string, identity types.AuthIdentity) IdentityKey {
	return IdentityKey{
		Service:  identityAuthorityService(service, identity),
		AuthType: identity.AuthType,
		Provider: identity.Provider,
		Subject:  identity.ProviderSubject,
	}
}

type eventTransition struct {
	increment bool
	block     bool
}

func validateEvent(event types.CasdoorEvent) (eventTransition, error) {
	if strings.TrimSpace(event.ID) == "" || event.EventOrder < 0 {
		return eventTransition{}, ErrInvalidEvent
	}
	key := eventIdentityKey(event)
	if err := key.validate(); err != nil || event.Provider != types.AuthProviderCasdoor {
		return eventTransition{}, ErrInvalidEvent
	}
	switch strings.ToLower(strings.TrimSpace(event.EventType)) {
	case "login", "signup":
		if event.Blocked {
			return eventTransition{increment: true, block: true}, nil
		}
		return eventTransition{}, nil
	case "logout", "sso-logout", "update-user":
		return eventTransition{increment: true, block: event.Blocked}, nil
	case "delete-user", "unlink":
		return eventTransition{increment: true, block: true}, nil
	default:
		return eventTransition{}, fmt.Errorf("%w: %s", ErrInvalidEvent, event.EventType)
	}
}

func eventFingerprint(event types.CasdoorEvent) (string, error) {
	payload := struct {
		ID              string         `json:"id"`
		ServiceName     string         `json:"service_name"`
		AuthType        types.AuthType `json:"auth_type"`
		Provider        string         `json:"provider"`
		ProviderSubject string         `json:"provider_subject"`
		UID             string         `json:"uid"`
		EventType       string         `json:"event_type"`
		EventOrder      int64          `json:"event_order"`
		Blocked         bool           `json:"blocked"`
		OccurredAt      int64          `json:"occurred_at"`
	}{
		ID: event.ID, ServiceName: event.ServiceName, AuthType: event.AuthType,
		Provider: event.Provider, ProviderSubject: event.ProviderSubject, UID: event.UID,
		EventType: strings.ToLower(strings.TrimSpace(event.EventType)), EventOrder: event.EventOrder,
		Blocked: event.Blocked, OccurredAt: event.OccurredAt.UnixNano(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
