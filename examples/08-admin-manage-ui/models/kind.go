package models

// 资料目录共用的分类枚举，供管理界面 Comvtp 分段筛选验证。
const (
	KindElectronics = 0
	KindBook        = 1
	KindAccessory   = 2
)

// KindTitle 返回分类的中文名称。
func KindTitle(kind int) string {
	switch kind {
	case KindElectronics:
		return "电子"
	case KindBook:
		return "图书"
	case KindAccessory:
		return "配件"
	default:
		return "未知"
	}
}

// KindValues 返回管理界面 ComBox 需要的全部分类键。
func KindValues() []int {
	return []int{KindElectronics, KindBook, KindAccessory}
}
