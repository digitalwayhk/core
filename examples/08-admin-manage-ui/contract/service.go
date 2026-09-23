// Package contract 保存管理界面能力目录可被任意层引用的无依赖契约。
// 本包不得导入其他包，也不得包含数据库模型、运行时对象或请求级状态。
package contract

// ServiceName 是本示例在配置、ServiceContext 和内部服务通信中的稳定名称。
const ServiceName = "catalog"
