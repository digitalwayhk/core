// 本文件独立声明与 07 既有表兼容的业务字段，不引用 Core 模型。
package reference

// Supplier 是原生对照服务直接交给 GORM 的记录。
type Supplier struct {
	Record
	UserID      uint   `gorm:"not null;index" json:"userID"`
	Code        string `gorm:"not null;uniqueIndex" json:"code"`
	Name        string `gorm:"not null" json:"name"`
	Description string `json:"description"`
	Enabled     bool   `gorm:"index" json:"enabled"`
}
