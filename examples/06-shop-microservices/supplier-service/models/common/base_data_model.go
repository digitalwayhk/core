// 本文件定义当前服务模型继承树的公共基础能力。
package common

// BaseDataModel 定义本文件能力使用的核心结构。
type BaseDataModel struct {
	*SupplierServiceModel
}

// NewBaseDataModel 执行本文件能力对应的业务操作。
func NewBaseDataModel() *BaseDataModel {
	return &BaseDataModel{SupplierServiceModel: NewSupplierServiceModel()}
}
