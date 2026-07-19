// Package transaction 提供 07 订单服务到框架 ReliableWriteStore 的领域适配。
// 该适配层只保留订单校验、用户维度查询和本地元数据准备；
// Group Commit、背压、pending ACK、重试保留和磁盘指标统一由框架实现。
package transaction

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
)

var (
	// ErrOrderWriteStoreUnavailable 表示当前服务实例尚未绑定订单可靠 store。
	ErrOrderWriteStoreUnavailable = errors.New("订单可靠写入存储不可用")
)

// OrderWriteAccess 定义 API/business 所需的最小实例级可靠写能力。
// 业务依赖该接口而不是具体 store，因此无法重新绑定 target、关闭资源或执行 Admin 物理删除。
type OrderWriteAccess interface {
	// Save 在当前副本完成 Badger 可靠提交，不承诺 MySQL 已经可见。
	Save(context.Context, *Order) error
	// FindLocalByRequest 只查询当前副本未汇合或尚未清理的本地订单。
	FindLocalByRequest(context.Context, uint, string) (*Order, error)
	// PendingByUser 返回当前副本指定用户的本地可见订单。
	PendingByUser(context.Context, uint) ([]*Order, error)
	// ForceSyncBatch 执行一轮有界汇合，limit 是本轮最多处理的 pending 数。
	ForceSyncBatch(context.Context, int) (nosql.ForceSyncResult, error)
	// Metrics 返回当前副本的批提交、背压、pending、磁盘和同步快照。
	Metrics() nosql.ReliableWriteMetrics
}

// OrderWriteStore 负责订单校验、用户维度本地查询和 ReliableWriteStore 委托。
// 它不保存远程 MySQL 连接，也不自建 batcher、guard 或同步 goroutine。
type OrderWriteStore struct {
	reliable *nosql.ReliableWriteStore[Order]
}

// NewOrderWriteStore 创建按服务实例身份隔离的订单可靠写入适配器。
// 框架同时返回的 Admin handle 被有意隔离：普通业务不得通过 PurgeLocal
// 物理删除 pending 并跳过应有的远程业务语义。
func NewOrderWriteStore(
	identity nosql.ServiceIdentity,
	config nosql.ReliableWriteStoreConfig,
) (*OrderWriteStore, error) {
	// 领域适配层只持有日常可靠写门面，Admin handle 仅应由独立运维入口持有。
	reliable, _, err := nosql.NewReliableWriteStore[Order](identity, config)
	if err != nil {
		return nil, err
	}
	return &OrderWriteStore{reliable: reliable}, nil
}

// UseWriteBehind 绑定当前实例唯一的远端订单写回目标。
// 重复绑定会返回框架错误，不允许运行中静默切换 MySQL 汇合语义。
func (store *OrderWriteStore) UseWriteBehind(target nosql.WriteBehindTarget[Order]) error {
	if store == nil || store.reliable == nil {
		return ErrOrderWriteStoreUnavailable
	}
	return store.reliable.UseWriteBehind(target)
}

// Save 校验并可靠保存订单；返回 nil 只表示订单已完成当前副本的 Badger SyncWrites。
// 此时进程异常后数据可恢复，但订单不一定已在 MySQL 权威库中可见。
func (store *OrderWriteStore) Save(ctx context.Context, order *Order) error {
	// 先拒绝无法形成稳定 Badger key 的输入，避免将无效事实进入可重试 pending。
	if order == nil {
		return errors.New("订单不能为空")
	}
	if order.GetID() == 0 {
		return errors.New("订单 ID 不能为空")
	}
	if err := order.validate(); err != nil {
		return err
	}
	// 在本地提交前统一时间精度、hash 和接单时间，保证重试使用稳定快照。
	order.prepareForLocalInsert()
	return store.reliable.Save(ctx, order)
}

// FindLocalByRequest 按用户幂等键查找当前实例本地订单。
// 它不查询 MySQL，只用于在当前副本的 pending 窗口内复用已接受订单。
func (store *OrderWriteStore) FindLocalByRequest(
	ctx context.Context,
	userID uint,
	requestID string,
) (*Order, error) {
	requestID = strings.TrimSpace(requestID)
	key := orderRequestLocalKey(userID, requestID)
	if key == "" {
		return nil, nil
	}
	item, err := store.reliable.GetLocal(ctx, key)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != userID || strings.TrimSpace(item.RequestID) != requestID {
		return nil, nil
	}
	// 请求指纹是否一致由 business 判断；store 只返回原始本地事实。
	return item, nil
}

// PendingByUser 返回当前实例本地可见的指定用户订单。
// 本地 tombstone 已由 ReliableWriteStore 过滤，因此返回值可直接与 MySQL 查询结果合并。
func (store *OrderWriteStore) PendingByUser(ctx context.Context, userID uint) ([]*Order, error) {
	// 键前缀是第一层缩小范围，不暴露原始 UserID，也不扫描其他用户的 pending。
	items, err := store.reliable.ScanLocal(ctx, nosql.LocalScanOptions{Prefix: OrderPendingUserPrefix(userID)})
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(items))
	for _, item := range items {
		// 再次校验 UserID 是防御性边界，不把前缀命中当作最终授权事实。
		if item != nil && item.UserID == userID {
			result = append(result, item)
		}
	}
	// 与远程订单查询保持一致，让最新订单在合并结果中优先。
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

// ForceSyncBatch 最多同步 limit 条本地 pending。
// limit 是单轮上限，不是无视返回时间的全量 drain；成功 key 由框架立即 ACK。
func (store *OrderWriteStore) ForceSyncBatch(ctx context.Context, limit int) (nosql.ForceSyncResult, error) {
	if store == nil || store.reliable == nil {
		return nosql.ForceSyncResult{}, ErrOrderWriteStoreUnavailable
	}
	return store.reliable.ForceSyncBatch(ctx, limit)
}

// Metrics 返回当前实例的统一可靠写入指标。
// nil store 返回空快照，不会向调用方暴露内部 ReliableWriteStore 指针。
func (store *OrderWriteStore) Metrics() nosql.ReliableWriteMetrics {
	if store == nil || store.reliable == nil {
		return nosql.ReliableWriteMetrics{}
	}
	return store.reliable.Metrics()
}

// Close 排空已接收本地提交并关闭当前订单 prefix，不强制访问远端 MySQL。
// 未汇合 pending 保留在实例目录中，并由关闭错误向生命周期 owner 报告。
func (store *OrderWriteStore) Close(ctx context.Context) error {
	if store == nil || store.reliable == nil {
		return nil
	}
	return store.reliable.Close(ctx)
}

// prepareForLocalInsert 在首次本地可靠提交前固定存储元数据，保证后续 at-least-once 重试使用同一业务快照。
func (order *Order) prepareForLocalInsert() {
	// Badger 与 MySQL 统一到秒精度；如果调用方已给出 CreatedAt，则保留该业务时间。
	now := time.Now().UTC().Truncate(time.Second)
	if order.CreatedAt != nil {
		now = order.CreatedAt.UTC().Truncate(time.Second)
	}
	// 首次本地提交同时建立创建/更新时间和稳定业务 hash。
	order.SetCreatedAt(now)
	order.SetUpdatedAt(now)
	order.SetHashcode(order.GetHash())
	// 已有 AcceptedAt 表示同一接单事实的恢复或重试，不应被当前时间覆盖。
	if order.AcceptedAt.IsZero() {
		order.AcceptedAt = now
	}
}
