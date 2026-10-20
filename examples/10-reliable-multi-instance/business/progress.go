// Package business 保存演示用的有界进度；它不是持久化业务 Inbox。
package business

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/digitalwayhk/core/examples/10-reliable-multi-instance/contract"
	"github.com/digitalwayhk/core/pkg/server/event"
)

const progressLimit = 256

type Receipt struct {
	EventID     string    `json:"eventID"`
	UserID      string    `json:"-"`
	Sequence    int       `json:"sequence"`
	State       string    `json:"state"`
	Attempts    int       `json:"attempts"`
	StartedAt   time.Time `json:"startedAt"`
	CompletedAt time.Time `json:"completedAt"`
}
type Progress struct {
	mu    sync.Mutex
	items map[string]Receipt
}

func NewProgress() *Progress { return &Progress{items: map[string]Receipt{}} }

// Current 属于本进程单个演示服务，不保存全体实例的业务权威状态。
var Current = NewProgress()

func (p *Progress) Handle(ctx context.Context, env *event.Envelope) error {
	var job contract.Job
	if err := json.Unmarshal(env.Data, &job); err != nil {
		return err
	}
	if job.UserID == "" || env.ID == "" || env.ShardKey != "user:"+job.UserID || job.Sequence <= 0 || job.DelayMillis < 0 || job.DelayMillis > 5000 {
		return errors.New("演示任务格式无效")
	}
	p.mu.Lock()
	item, exists := p.items[env.ID]
	if !exists && len(p.items) >= progressLimit {
		oldest := ""
		var at time.Time
		for id, record := range p.items {
			if record.State == "completed" && (oldest == "" || record.CompletedAt.Before(at)) {
				oldest, at = id, record.CompletedAt
			}
		}
		if oldest == "" {
			p.mu.Unlock()
			return errors.New("演示进度已满")
		}
		delete(p.items, oldest)
	}
	item.EventID, item.UserID, item.Sequence = env.ID, job.UserID, job.Sequence
	item.Attempts++
	item.State = "running"
	item.StartedAt = time.Now().UTC()
	p.items[env.ID] = item
	p.mu.Unlock()
	timer := time.NewTimer(time.Duration(job.DelayMillis) * time.Millisecond)
	defer timer.Stop()
	var err error
	select {
	case <-timer.C:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err == nil && job.FailFirst && item.Attempts == 1 {
		err = errors.New("演示：本副本首次处理失败，等待 Core 重投")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	item = p.items[env.ID]
	if err != nil {
		item.State = "failed"
	} else {
		item.State = "completed"
		item.CompletedAt = time.Now().UTC()
	}
	p.items[env.ID] = item
	return err
}
func (p *Progress) ForUser(user string) []Receipt {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]Receipt, 0)
	for _, item := range p.items {
		if item.UserID == user {
			result = append(result, item)
		}
	}
	return result
}
