// 本文件定义 06 微服务示例事件通道使用的跨服务消息 DTO 能力。
package event

// ProductChanged 定义本文件能力使用的核心结构。
type ProductChanged struct {
	Metadata
	SupplierID uint   `json:"supplierID"`
	ProductID  uint   `json:"productID"`
	Action     string `json:"action"`
}
