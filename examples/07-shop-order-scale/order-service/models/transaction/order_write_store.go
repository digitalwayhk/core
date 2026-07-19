// Package transaction 提供 07 订单服务到框架 ReliableWriteStore 的领域适配。
package transaction

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
)

var (
	// ErrOrderWriteStoreUnavailable 表示当前服务实例尚未绑定订单可靠 store。
	ErrOrderWriteStoreUnavailable = errors.New("订单可靠写入存储不可用")
)

// OrderWriteAccess 定义订单业务所需的最小实例级本地可靠写入能力。
type OrderWriteAccess interface {
	Save(context.Context, *Order) error
	FindLocalByRequest(context.Context, uint, string) (*Order, error)
	PendingByUser(context.Context, uint) ([]*Order, error)
	ForceSyncBatch(context.Context, int) (nosql.ForceSyncResult, error)
	Metrics() nosql.ReliableWriteMetrics
}

// OrderWriteStore 负责订单校验、用户维度本地查询和 ReliableWriteStore 委托。
type OrderWriteStore struct {
	reliable *nosql.ReliableWriteStore[Order]
}

// NewOrderWriteStore 创建按服务实例身份隔离的订单可靠写入适配器。
func NewOrderWriteStore(
	identity nosql.ServiceIdentity,
	config nosql.ReliableWriteStoreConfig,
) (*OrderWriteStore, error) {
	reliable, _, err := nosql.NewReliableWriteStore[Order](identity, config)
	if err != nil {
		return nil, err
	}
	return &OrderWriteStore{reliable: reliable}, nil
}

// UseWriteBehind 绑定当前实例唯一的远端订单写回目标。
func (store *OrderWriteStore) UseWriteBehind(target nosql.WriteBehindTarget[Order]) error {
	if store == nil || store.reliable == nil {
		return ErrOrderWriteStoreUnavailable
	}
	return store.reliable.UseWriteBehind(target)
}

// Save 校验并可靠保存订单；成功只表示当前实例本地提交完成。
func (store *OrderWriteStore) Save(ctx context.Context, order *Order) error {
	if order == nil {
		return errors.New("订单不能为空")
	}
	if order.GetID() == 0 {
		return errors.New("订单 ID 不能为空")
	}
	if err := order.validate(); err != nil {
		return err
	}
	order.prepareForLocalInsert()
	return store.reliable.Save(ctx, order)
}

// FindLocalByRequest 按用户幂等键查找当前实例本地订单。
func (store *OrderWriteStore) FindLocalByRequest(
	ctx context.Context,
	userID uint,
	requestID string,
) (*Order, error) {
	items, err := store.PendingByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item != nil && item.RequestID == requestID {
			return item, nil
		}
	}
	return nil, nil
}

// PendingByUser 返回当前实例本地可见的指定用户订单。
func (store *OrderWriteStore) PendingByUser(ctx context.Context, userID uint) ([]*Order, error) {
	items, err := store.reliable.ScanLocal(ctx, nosql.LocalScanOptions{Prefix: OrderPendingUserPrefix(userID)})
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(items))
	for _, item := range items {
		if item != nil && item.UserID == userID {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

// ForceSyncBatch 最多同步 limit 条本地 pending。
func (store *OrderWriteStore) ForceSyncBatch(ctx context.Context, limit int) (nosql.ForceSyncResult, error) {
	if store == nil || store.reliable == nil {
		return nosql.ForceSyncResult{}, ErrOrderWriteStoreUnavailable
	}
	return store.reliable.ForceSyncBatch(ctx, limit)
}

// Metrics 返回当前实例的统一可靠写入指标。
func (store *OrderWriteStore) Metrics() nosql.ReliableWriteMetrics {
	if store == nil || store.reliable == nil {
		return nosql.ReliableWriteMetrics{}
	}
	return store.reliable.Metrics()
}

// Close 排空已接收本地提交并关闭当前订单 prefix，不强制访问远端 MySQL。
func (store *OrderWriteStore) Close(ctx context.Context) error {
	if store == nil || store.reliable == nil {
		return nil
	}
	return store.reliable.Close(ctx)
}

func (order *Order) prepareForLocalInsert() {
	now := time.Now().UTC().Truncate(time.Second)
	if order.CreatedAt != nil {
		now = order.CreatedAt.UTC().Truncate(time.Second)
	}
	order.SetCreatedAt(now)
	order.SetUpdatedAt(now)
	order.SetHashcode(order.GetHash())
	if order.AcceptedAt.IsZero() {
		order.AcceptedAt = now
	}
}
