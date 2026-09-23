package manage

import (
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	"github.com/digitalwayhk/core/service/manage/view"
)

// applyKindField 把分类枚举配置成 Comvtp 分段筛选和表单下拉。
func applyKindField(field *view.FieldModel) {
	if !field.IsFieldOrTitle("Kind") {
		return
	}
	field.Title = "分类类型"
	field.ShowInComvtp = true
	for _, kind := range models.KindValues() {
		field.ComBoxValue(kind, models.KindTitle(kind))
	}
}
