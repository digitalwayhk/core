// Package contract 保存商城可被任意层和其他服务引用的无依赖契约。
// 本包不得导入其他包，也不得包含数据库模型、运行时对象或请求级状态。
package contract

// ServiceName 是商城服务在配置、ServiceContext 和内部服务通信中的稳定名称。
const ServiceName = "shop"
