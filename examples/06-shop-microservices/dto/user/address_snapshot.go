// 本文件定义 06 微服务示例用户域对外传递的 DTO 能力。
package user

// AddressSnapshot 定义本文件能力使用的核心结构。
type AddressSnapshot struct {
	AddressID uint   `json:"addressID"`
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}
