package identity

import (
	"context"
	"sync"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/contract"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// IdentityEventService 将框架标准 Casdoor 事件幂等写入业务审计表。
type IdentityEventService struct {
	mu sync.Mutex
}

// NewIdentityEventService 创建身份事件审计服务。
func NewIdentityEventService() *IdentityEventService { return &IdentityEventService{} }

// Record 保存一条已验证的身份事件；重复 EventID 保持幂等。
func (own *IdentityEventService) Record(ctx context.Context, event types.CasdoorEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if event.ServiceName != contract.ServiceName || event.Provider != types.AuthProviderCasdoor {
		return models.NewBusinessError("身份事件不属于当前服务")
	}
	record := models.NewIdentityEventRecord()
	record.EventID = event.ID
	record.AuthType = string(event.AuthType)
	record.UserID = event.UID
	record.EventType = event.EventType
	record.Generation = event.Generation
	record.Blocked = event.Blocked
	record.OccurredAt = event.OccurredAt
	if err := record.Normalize(); err != nil {
		return err
	}

	own.mu.Lock()
	defer own.mu.Unlock()
	existing, err := models.NewIdentityEventRecord().QueryByEventID(record.EventID)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		if sameIdentityEvent(existing[0], record) {
			return nil
		}
		return models.NewBusinessError("身份事件 ID 对应内容冲突")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return record.Insert()
}

func sameIdentityEvent(left, right *models.IdentityEventRecord) bool {
	return left != nil && right != nil &&
		left.EventID == right.EventID && left.AuthType == right.AuthType &&
		left.UserID == right.UserID && left.EventType == right.EventType &&
		left.Generation == right.Generation && left.Blocked == right.Blocked &&
		left.OccurredAt.Equal(right.OccurredAt)
}
