// 本文件定义 06 微服务示例供应商域对外传递的 DTO 能力。
package supplier

// Supplier 定义本文件能力使用的核心结构。
type Supplier struct {
	ID          uint   `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}
