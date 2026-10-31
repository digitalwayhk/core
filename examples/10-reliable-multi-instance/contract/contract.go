// Package contract 定义演示事件与逻辑服务，不包含 Broker 实现细节。
package contract

import (
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
)

const (
	ProducerService = "event-submit"
	WorkerService   = "event-worker"
	Subject         = "example10.jobs.v1"
	EventType       = "job.requested.v1"
)

type Job struct {
	UserID      string `json:"userID"`
	Sequence    int    `json:"sequence"`
	DelayMillis int    `json:"delayMillis"`
	FailFirst   bool   `json:"failFirst"`
}

// Lifecycle 是本演示明确选择的应用策略，两个副本共享一个实际逻辑消费组。
func Lifecycle() mq.LifecyclePolicy {
	return mq.LifecyclePolicy{Subject: Subject, Mode: mq.LifecycleModeEnforce, RequiredGroups: []mq.ConsumerGroupRequirement{{Name: WorkerService, Start: mq.StartFromAllRetained}}, Retention: mq.RetentionPolicy{MinAge: time.Second}, Capacity: mq.CapacityPolicy{HardMessages: 10000}, Reclaim: mq.ReclaimBudget{Interval: time.Second, BatchSize: 100, TimeBudget: 100 * time.Millisecond}}
}
