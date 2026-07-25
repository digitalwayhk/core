// 本文件定义 06 微服务示例事件通道使用的跨服务消息 DTO 能力。
package event

// SupplierChanged 定义本文件能力使用的核心结构。
type SupplierChanged struct {
	Metadata
	SupplierID uint   `json:"supplierID"`
	Action     string `json:"action"`
}
