// 本文件定义当前服务模型继承树的公共基础能力。
package common

// BusinessModel 定义本文件能力使用的核心结构。
type BusinessModel struct {
	*OrderServiceModel
}

// NewBusinessModel 执行本文件能力对应的业务操作。
func NewBusinessModel() *BusinessModel {
	return &BusinessModel{OrderServiceModel: NewOrderServiceModel()}
}
