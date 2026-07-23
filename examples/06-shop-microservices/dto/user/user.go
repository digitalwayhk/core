// 本文件定义 06 微服务示例用户域对外传递的 DTO 能力。
package user

// User 定义本文件能力使用的核心结构。
type User struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}
