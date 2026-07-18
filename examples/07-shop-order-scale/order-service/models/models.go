// Package models 是 07 订单服务模型层的兼容门面。
package models

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/basedata"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/schema"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

type (
	// DataAction 是模型事务函数使用的数据访问接口别名。
	DataAction = persistencetypes.IDataAction

	// ServiceBaseModel 是订单服务级基础模型别名。
	ServiceBaseModel = common.ServiceBaseModel

	// RuntimeStampedModel 是水平扩展运行时戳模型别名。
	RuntimeStampedModel = common.RuntimeStampedModel

	// OrderRule 是共享远程权威库中的订单规则模型别名。
	OrderRule = basedata.OrderRule

	// Order 是共享远程权威库中的订单事实模型别名。
	Order = transaction.Order

	// LocalPendingOrder 是当前 order 实例的本地 pending 模型别名。
	LocalPendingOrder = transaction.LocalPendingOrder

	// OutboxRecord 是当前 order 实例的本地 Outbox 模型别名。
	OutboxRecord = transaction.OutboxRecord

	// OutboxStore 是标准 EventBridge 使用的本地 Outbox 适配器别名。
	OutboxStore = transaction.OutboxStore
)

const (
	// OrderStatusAccepted 表示订单已被本地可靠接收。
	OrderStatusAccepted = transaction.OrderStatusAccepted

	// OrderStatusSynced 表示订单已同步到远程权威库。
	OrderStatusSynced = transaction.OrderStatusSynced

	// OrderStatusCancelled 表示订单已撤销。
	OrderStatusCancelled = transaction.OrderStatusCancelled

	// PaymentStatusUnpaid 表示订单尚未支付。
	PaymentStatusUnpaid = transaction.PaymentStatusUnpaid

	// PaymentStatusPaid 表示订单已支付。
	PaymentStatusPaid = transaction.PaymentStatusPaid

	// PendingStatusAccepted 表示 pending 已可靠接收。
	PendingStatusAccepted = transaction.PendingStatusAccepted

	// PendingStatusSynced 表示 pending 已同步成功。
	PendingStatusSynced = transaction.PendingStatusSynced

	// PendingStatusFailed 表示 pending 最近一次同步失败。
	PendingStatusFailed = transaction.PendingStatusFailed
)

var (
	// NewServiceBaseModel 创建服务级基础模型。
	NewServiceBaseModel = common.NewServiceBaseModel

	// NewRuntimeStampedModel 创建运行时戳模型。
	NewRuntimeStampedModel = common.NewRuntimeStampedModel

	// NewOrderRule 创建订单规则模型。
	NewOrderRule = basedata.NewOrderRule

	// GetEnabledOrderRuleWith 从远程权威库读取启用规则。
	GetEnabledOrderRuleWith = basedata.GetEnabledOrderRuleWith

	// SaveOrderRuleWith 保存远程权威库订单规则。
	SaveOrderRuleWith = basedata.SaveOrderRuleWith

	// NewOrder 创建订单事实模型。
	NewOrder = transaction.NewOrder

	// FindRemoteOrderByIdempotencyWith 按幂等键查询远程订单。
	FindRemoteOrderByIdempotencyWith = transaction.FindRemoteOrderByIdempotencyWith

	// UpsertRemoteOrderWith 幂等写入远程订单。
	UpsertRemoteOrderWith = transaction.UpsertRemoteOrderWith

	// NewLocalPendingOrder 创建本地 pending 订单模型。
	NewLocalPendingOrder = transaction.NewLocalPendingOrder

	// FindLocalPendingByRequest 按 UserID + requestID 查询本地 pending。
	FindLocalPendingByRequest = transaction.FindLocalPendingByRequest

	// PendingLocalOrders 读取待同步本地 pending。
	PendingLocalOrders = transaction.PendingLocalOrders

	// MarkPendingSyncedWith 标记本地 pending 已同步。
	MarkPendingSyncedWith = transaction.MarkPendingSyncedWith

	// MarkPendingFailedWith 标记本地 pending 同步失败。
	MarkPendingFailedWith = transaction.MarkPendingFailedWith

	// NewOutbox 创建本地 Outbox 模型。
	NewOutbox = transaction.NewOutbox

	// NewOutboxRecord 创建本地 Outbox 事件记录。
	NewOutboxRecord = transaction.NewOutboxRecord
)

// EnsureStorage 确保 07 订单服务本地库和远程权威库完成建表。
func EnsureStorage() error { return schema.EnsureStorage() }

// RunLocalTransaction 在当前 order 实例本地库执行事务。
func RunLocalTransaction(operation func(persistencetypes.IDataAction) error) error {
	return store.RunLocalTransaction(schema.EnsureStorage, operation)
}

// RunRemoteTransaction 在共享远程权威库执行事务。
func RunRemoteTransaction(operation func(persistencetypes.IDataAction) error) error {
	return store.RunRemoteTransaction(schema.EnsureStorage, operation)
}
