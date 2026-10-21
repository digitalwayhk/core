// 本文件保存同事务消费幂等依据；示例保留记录，不宣称无限保留策略适合所有项目。
package models

type appliedEvent struct {
	ID      string `gorm:"primaryKey;size:128"`
	Scope   string `gorm:"size:128;index:scope_key"`
	KeyName string `gorm:"size:128;index:scope_key"`
}
