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

	// PaymentType 是共享远程权威库中的支付类型模型别名。
	PaymentType = basedata.PaymentType

	// Order 是共享远程权威库中的订单事实模型别名。
	Order = transaction.Order

	// OutboxRecord 是 MySQL 权威库中的 Outbox 模型别名。
	OutboxRecord = transaction.OutboxRecord

	// OutboxStore 是标准 EventBridge 使用的 MySQL Outbox 适配器别名。
	OutboxStore = transaction.OutboxStore

	// OrderQueryFilter 是远程权威订单查询条件别名。
	OrderQueryFilter = transaction.OrderQueryFilter
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

	// NewPaymentType 创建支付类型模型。
	NewPaymentType = basedata.NewPaymentType

	// ListPaymentTypesWith 读取远程权威库支付类型列表。
	ListPaymentTypesWith = basedata.ListPaymentTypesWith

	// FindPaymentTypeWith 按 ID 读取远程权威库支付类型。
	FindPaymentTypeWith = basedata.FindPaymentTypeWith

	// SavePaymentTypeWith 保存远程权威库支付类型。
	SavePaymentTypeWith = basedata.SavePaymentTypeWith

	// NewOrder 创建订单事实模型。
	NewOrder = transaction.NewOrder

	// FindRemoteOrderByIdempotencyWith 按幂等键查询远程订单。
	FindRemoteOrderByIdempotencyWith = transaction.FindRemoteOrderByIdempotencyWith

	// UpsertRemoteOrderWith 幂等写入远程订单。
	UpsertRemoteOrderWith = transaction.UpsertRemoteOrderWith

	// ListRemoteOrdersWith 分页查询远程权威订单。
	ListRemoteOrdersWith = transaction.ListRemoteOrdersWith

	// CancelRemoteOrderWith 撤销远程权威订单。
	CancelRemoteOrderWith = transaction.CancelRemoteOrderWith

	// PayRemoteOrderWith 支付远程权威订单。
	PayRemoteOrderWith = transaction.PayRemoteOrderWith

	// AddOrder 将订单写入当前实例 Badger 可靠层。
	AddOrder = transaction.AddOrder

	// UseOrderWriteBehind 绑定本地订单 pending 的远端汇合目标。
	UseOrderWriteBehind = transaction.UseOrderWriteBehind

	// SyncLocalOrders 触发本地订单 pending 汇合到远端目标。
	SyncLocalOrders = transaction.SyncLocalOrders

	// FindLocalOrderByRequest 按 UserID + requestID 查询当前实例本地订单。
	FindLocalOrderByRequest = transaction.FindLocalOrderByRequest

	// PendingLocalOrders 读取当前实例待汇合本地订单。
	PendingLocalOrders = transaction.PendingLocalOrders

	// RemoveLocalOrder 删除已成功汇合的本地订单。
	RemoveLocalOrder = transaction.RemoveLocalOrder

	// PendingOrdersByUser 查询当前实例指定用户本地未汇合订单。
	PendingOrdersByUser = transaction.PendingOrdersByUser

	// StartOrderWriteStore 启动当前实例 Badger 可靠写入层。
	StartOrderWriteStore = transaction.StartOrderWriteStore

	// StopOrderWriteStore 停止当前实例 Badger 可靠写入层。
	StopOrderWriteStore = transaction.StopOrderWriteStore

	// GetOrderWritePerformanceSnapshot 返回当前实例写入指标。
	GetOrderWritePerformanceSnapshot = transaction.GetOrderWritePerformanceSnapshot

	// NewOutbox 创建 MySQL Outbox 模型。
	NewOutbox = transaction.NewOutbox

	// NewOutboxRecord 创建 MySQL Outbox 事件记录。
	NewOutboxRecord = transaction.NewOutboxRecord

	// InsertOutboxIfMissingWith 幂等写入 MySQL Outbox。
	InsertOutboxIfMissingWith = transaction.InsertOutboxIfMissingWith
)

// EnsureStorage 确保 07 订单服务 MySQL 权威库完成建表。
func EnsureStorage() error { return schema.EnsureStorage() }

// RunRemoteTransaction 在共享远程权威库执行事务。
func RunRemoteTransaction(operation func(persistencetypes.IDataAction) error) error {
	return store.RunRemoteTransaction(schema.EnsureStorage, operation)
}
