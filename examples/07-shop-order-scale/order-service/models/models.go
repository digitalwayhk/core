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

	// OrderWriteAccess 是 API/business 注入的最小实例级可靠写接口别名。
	// 别名只稳定跨层依赖方向，不向根 models 包转移 store 创建、绑定或关闭职责。
	OrderWriteAccess = transaction.OrderWriteAccess

	// OrderWriteRuntime 是服务实例持有的 typed runtime 别名。
	// 它作为路由构造期的稳定引用，实际 store 仍由 Service.Start 绑定并交给 ServiceContext 托管。
	OrderWriteRuntime = transaction.OrderWriteRuntime
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
	// ErrOrderWriteStoreUnavailable 表示当前服务实例订单可靠 store 不可用。
	ErrOrderWriteStoreUnavailable = transaction.ErrOrderWriteStoreUnavailable
	// ErrOrderRuleNotFound 表示权威库尚未配置指定订单规则。
	ErrOrderRuleNotFound = basedata.ErrOrderRuleNotFound

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
	// UpsertRemoteOrdersWith 幂等批量写入远程订单。
	UpsertRemoteOrdersWith = transaction.UpsertRemoteOrdersWith

	// ListRemoteOrdersWith 分页查询远程权威订单。
	ListRemoteOrdersWith = transaction.ListRemoteOrdersWith

	// CancelRemoteOrderWith 撤销远程权威订单。
	CancelRemoteOrderWith = transaction.CancelRemoteOrderWith

	// PayRemoteOrderWith 支付远程权威订单。
	PayRemoteOrderWith = transaction.PayRemoteOrderWith

	// NewOrderWriteRuntime 创建实例级订单可靠写入 runtime。
	NewOrderWriteRuntime = transaction.NewOrderWriteRuntime

	// NewOutbox 创建 MySQL Outbox 模型。
	NewOutbox = transaction.NewOutbox

	// NewOutboxRecord 创建 MySQL Outbox 事件记录。
	NewOutboxRecord = transaction.NewOutboxRecord

	// InsertOutboxIfMissingWith 幂等写入 MySQL Outbox。
	InsertOutboxIfMissingWith = transaction.InsertOutboxIfMissingWith
	// InsertOutboxesIfMissingWith 幂等批量写入 MySQL Outbox。
	InsertOutboxesIfMissingWith = transaction.InsertOutboxesIfMissingWith
)

// EnsureStorage 确保 07 订单服务 MySQL 权威库完成建表。
func EnsureStorage() error { return schema.EnsureStorage() }

// RemoteDataAction 返回 07 订单服务共享 MySQL 权威库的数据访问器。
// Manage API 应将其传给 entity.NewModelList，让 Core 保留标准查询、排序和分页语义。
func RemoteDataAction() persistencetypes.IDataAction { return store.GetRemote() }

// RunRemoteTransaction 在共享远程权威库执行事务。
func RunRemoteTransaction(operation func(persistencetypes.IDataAction) error) error {
	return store.RunRemoteTransaction(schema.EnsureStorage, operation)
}
