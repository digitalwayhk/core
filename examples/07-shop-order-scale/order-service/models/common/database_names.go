// Package common 定义 07 订单服务模型层共享的数据库命名能力。
package common

const (
	// RemoteDatabaseName 是所有 order 实例共享的远程权威库名。
	RemoteDatabaseName = "shop-order-scale-remote"
)
