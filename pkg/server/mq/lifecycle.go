package mq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// 生命周期稳定错误。
var (
	ErrLifecycleUnsupported           = errors.New("mq: message lifecycle unsupported")
	ErrLifecyclePolicyInvalid         = errors.New("mq: message lifecycle policy invalid")
	ErrLifecyclePolicyConflict        = errors.New("mq: message lifecycle policy conflict")
	ErrLifecycleRequiredGroupMismatch = errors.New("mq: message lifecycle required group mismatch")
	ErrLifecycleStateUncertain        = errors.New("mq: message lifecycle state uncertain")
	ErrLifecycleUnsafeBrokerPolicy    = errors.New("mq: message lifecycle broker policy is unsafe")
	ErrLifecycleBackpressure          = errors.New("mq: message lifecycle capacity reached")
	ErrLifecycleCapacityUnknown       = errors.New("mq: message lifecycle capacity unknown")
)

// LifecycleMode 控制策略只观测还是允许物理回收。
type LifecycleMode string

const (
	// LifecycleModeObserve 只建立必需组和快照，不物理删除消息。
	LifecycleModeObserve LifecycleMode = "observe"
	// LifecycleModeEnforce 在全部安全条件成立时执行有界回收。
	LifecycleModeEnforce LifecycleMode = "enforce"
)

// PublishAckLevel 区分 Broker 只接受请求与已确认持久化的发布语义。
type PublishAckLevel string

const (
	// PublishAckBrokerAccepted 表示 Broker 已接受发布，但不承诺已落盘。
	PublishAckBrokerAccepted PublishAckLevel = "broker-accepted"
	// PublishAckBrokerPersisted 表示 Broker 原生 publish ACK 已确认持久化。
	PublishAckBrokerPersisted PublishAckLevel = "broker-persisted"
)

// ConsumerStartPosition 声明新必需组的首次消费起点。
type ConsumerStartPosition string

const (
	// StartFromAllRetained 从 Broker 当前仍保留的最早消息开始。
	StartFromAllRetained ConsumerStartPosition = "all-retained"
	// StartFromNew 从消费组创建时的末尾之后开始。
	StartFromNew ConsumerStartPosition = "new-only"
)

// LifecycleMetricState 表示 Broker 生命周期快照的可信状态。
type LifecycleMetricState string

const (
	// LifecycleStateOK 表示所有要求的状态均已采集。
	LifecycleStateOK LifecycleMetricState = "ok"
	// LifecycleStatePartial 表示只有部分状态可用。
	LifecycleStatePartial LifecycleMetricState = "partial"
	// LifecycleStateStale 表示快照已超过可用时间窗口。
	LifecycleStateStale LifecycleMetricState = "stale"
	// LifecycleStateUnavailable 表示 Broker 或生命周期管理面不可用。
	LifecycleStateUnavailable LifecycleMetricState = "unavailable"
	// LifecycleStateNotCollected 表示 Provider 不支持或尚未采集该值。
	LifecycleStateNotCollected LifecycleMetricState = "not_collected"
)

// ConsumerGroupRequirement 声明一个必须完成处理的逻辑消费组。
type ConsumerGroupRequirement struct {
	Name  string
	Start ConsumerStartPosition
}

// RetentionPolicy 声明消费完成后仍需保留的最小时间。
type RetentionPolicy struct {
	MinAge time.Duration
}

// RetryPolicy 声明 Handler 失败后的有界重投与死信边界。
type RetryPolicy struct {
	MaxDeliveries     int
	Backoff           []time.Duration
	DeadLetterSubject string
	HandlerTimeout    time.Duration
	MaxAckPending     int
}

// CapacityPolicy 声明 Subject 积压的软、硬背压阈值。
type CapacityPolicy struct {
	SoftMessages int64
	HardMessages int64
	SoftBytes    int64
	HardBytes    int64
}

// ReclaimBudget 限制生命周期 worker 的周期、单批数量和单轮时间。
type ReclaimBudget struct {
	Interval   time.Duration
	BatchSize  int
	TimeBudget time.Duration
}

// LifecyclePolicy 是应用对一个 Subject 声明的完整消息生命周期。
type LifecyclePolicy struct {
	Subject            string
	Mode               LifecycleMode
	RequiredPublishAck PublishAckLevel
	RequiredGroups     []ConsumerGroupRequirement
	Retention          RetentionPolicy
	Retry              RetryPolicy
	Capacity           CapacityPolicy
	Reclaim            ReclaimBudget
}

// Normalize 补充保守默认值并规范化组顺序。
func (p LifecyclePolicy) Normalize() LifecyclePolicy {
	p.Subject = strings.TrimSpace(p.Subject)
	if p.Mode == "" {
		p.Mode = LifecycleModeObserve
	}
	if p.RequiredPublishAck == "" {
		p.RequiredPublishAck = PublishAckBrokerAccepted
	}
	if p.Reclaim.Interval == 0 {
		p.Reclaim.Interval = 30 * time.Second
	}
	if p.Reclaim.BatchSize == 0 {
		p.Reclaim.BatchSize = 1000
	}
	if p.Reclaim.TimeBudget == 0 {
		p.Reclaim.TimeBudget = 500 * time.Millisecond
	}
	p.RequiredGroups = append([]ConsumerGroupRequirement(nil), p.RequiredGroups...)
	for i := range p.RequiredGroups {
		p.RequiredGroups[i].Name = strings.TrimSpace(p.RequiredGroups[i].Name)
	}
	sort.Slice(p.RequiredGroups, func(i, j int) bool {
		if p.RequiredGroups[i].Name != p.RequiredGroups[j].Name {
			return p.RequiredGroups[i].Name < p.RequiredGroups[j].Name
		}
		return p.RequiredGroups[i].Start < p.RequiredGroups[j].Start
	})
	p.Retry.Backoff = append([]time.Duration(nil), p.Retry.Backoff...)
	return p
}

// Validate 校验策略是否完整、有界且没有歧义。
func (p LifecyclePolicy) Validate() error {
	p = p.Normalize()
	if p.Subject == "" {
		return fmt.Errorf("%w: subject is required", ErrLifecyclePolicyInvalid)
	}
	if p.Mode != LifecycleModeObserve && p.Mode != LifecycleModeEnforce {
		return fmt.Errorf("%w: mode %q is invalid", ErrLifecyclePolicyInvalid, p.Mode)
	}
	if p.RequiredPublishAck != PublishAckBrokerAccepted && p.RequiredPublishAck != PublishAckBrokerPersisted {
		return fmt.Errorf("%w: publish ack level %q is invalid", ErrLifecyclePolicyInvalid, p.RequiredPublishAck)
	}
	if len(p.RequiredGroups) == 0 {
		return fmt.Errorf("%w: required groups are empty", ErrLifecyclePolicyInvalid)
	}
	seen := make(map[string]struct{}, len(p.RequiredGroups))
	for _, group := range p.RequiredGroups {
		if group.Name == "" {
			return fmt.Errorf("%w: required group name is empty", ErrLifecyclePolicyInvalid)
		}
		if group.Start != StartFromAllRetained && group.Start != StartFromNew {
			return fmt.Errorf("%w: required group %q start %q is invalid", ErrLifecyclePolicyInvalid, group.Name, group.Start)
		}
		if _, exists := seen[group.Name]; exists {
			return fmt.Errorf("%w: required group %q is duplicated", ErrLifecyclePolicyInvalid, group.Name)
		}
		seen[group.Name] = struct{}{}
	}
	if p.Retention.MinAge < 0 {
		return fmt.Errorf("%w: retention min age cannot be negative", ErrLifecyclePolicyInvalid)
	}
	if p.Reclaim.Interval <= 0 || p.Reclaim.BatchSize <= 0 || p.Reclaim.TimeBudget <= 0 {
		return fmt.Errorf("%w: reclaim interval, batch size and time budget must be positive", ErrLifecyclePolicyInvalid)
	}
	if err := validateRetryPolicy(p.Retry); err != nil {
		return err
	}
	if err := validateCapacityPolicy(p.Capacity); err != nil {
		return err
	}
	return nil
}

func validateRetryPolicy(policy RetryPolicy) error {
	if policy.MaxDeliveries < 0 || policy.HandlerTimeout < 0 || policy.MaxAckPending < 0 {
		return fmt.Errorf("%w: retry values cannot be negative", ErrLifecyclePolicyInvalid)
	}
	if policy.MaxDeliveries > 0 && strings.TrimSpace(policy.DeadLetterSubject) == "" {
		return fmt.Errorf("%w: dead letter subject is required for bounded retry", ErrLifecyclePolicyInvalid)
	}
	if policy.MaxDeliveries == 0 && strings.TrimSpace(policy.DeadLetterSubject) != "" {
		return fmt.Errorf("%w: max deliveries is required with dead letter subject", ErrLifecyclePolicyInvalid)
	}
	for _, delay := range policy.Backoff {
		if delay <= 0 {
			return fmt.Errorf("%w: retry backoff must be positive", ErrLifecyclePolicyInvalid)
		}
	}
	return nil
}

func validateCapacityPolicy(policy CapacityPolicy) error {
	if policy.SoftMessages < 0 || policy.HardMessages < 0 || policy.SoftBytes < 0 || policy.HardBytes < 0 {
		return fmt.Errorf("%w: capacity values cannot be negative", ErrLifecyclePolicyInvalid)
	}
	if policy.HardMessages > 0 && policy.SoftMessages > policy.HardMessages {
		return fmt.Errorf("%w: soft message limit exceeds hard limit", ErrLifecyclePolicyInvalid)
	}
	if policy.HardBytes > 0 && policy.SoftBytes > policy.HardBytes {
		return fmt.Errorf("%w: soft byte limit exceeds hard limit", ErrLifecyclePolicyInvalid)
	}
	return nil
}

// Fingerprint 返回包含所有安全相关字段的稳定策略指纹。
func (p LifecyclePolicy) Fingerprint() string {
	normalized := p.Normalize()
	// observe/enforce 只决定 Core 是否执行回收，不改变 Broker 侧安全策略。
	normalized.Mode = ""
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

// LifecycleCapabilities 明确 Provider 可以为哪些生命周期要求提供行为证据。
type LifecycleCapabilities struct {
	PublishAck       PublishAckLevel
	RequiredGroups   bool
	Retry            bool
	DeadLetter       bool
	SafeReclaim      bool
	RetainedMessages bool
	RetainedBytes    bool
	Pending          bool
	Lag              bool
	OldestAge        bool
}

// ConsumerGroupSnapshot 是一个必需消费组的低基数运行状态。
type ConsumerGroupSnapshot struct {
	Name               string
	State              LifecycleMetricState
	Pending            *int64
	Lag                *int64
	OldestAge          *time.Duration
	EnrollmentFrontier string
	CompletedFrontier  string
	LastProgressAt     *time.Time
}

// LifecycleSnapshot 是 Provider 为容量门禁和安全回收提供的只读快照。
type LifecycleSnapshot struct {
	Subject           string
	PolicyFingerprint string
	ObservedAt        time.Time
	State             LifecycleMetricState
	RetainedMessages  *int64
	RetainedBytes     *int64
	BacklogMessages   *int64
	PendingMessages   *int64
	OldestAge         *time.Duration
	SafeFrontier      string
	Groups            []ConsumerGroupSnapshot
}

// ReclaimResult 记录一轮有界回收的真实结果。
type ReclaimResult struct {
	Reclaimed       int64
	Duration        time.Duration
	BudgetExhausted bool
}

// LifecycleMQProvider 是 Provider 可选的统一生命周期管理能力。
type LifecycleMQProvider interface {
	MQProvider
	LifecycleCapabilities() LifecycleCapabilities
	EnsureLifecycle(ctx context.Context, policy LifecyclePolicy) error
	InspectLifecycle(ctx context.Context, policy LifecyclePolicy) (LifecycleSnapshot, error)
	ReclaimLifecycle(ctx context.Context, policy LifecyclePolicy, snapshot LifecycleSnapshot) (ReclaimResult, error)
}
