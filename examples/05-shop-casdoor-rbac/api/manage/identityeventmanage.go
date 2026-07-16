package manage

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"
	"github.com/digitalwayhk/core/service/manage/view"
)

// IdentityEventManage 为管理员提供标准身份事件的只读审计视图。
type IdentityEventManage struct {
	*BusinessManage[models.IdentityEventRecord]
}

// NewIdentityEventManage 创建只暴露 View 和 Search 的身份审计 Manage。
func NewIdentityEventManage() *IdentityEventManage {
	own := &IdentityEventManage{}
	own.BusinessManage = NewBusinessManage[models.IdentityEventRecord](own)
	return own
}

// ViewModel 设置身份事件审计页面。
func (*IdentityEventManage) ViewModel(model *view.ViewModel) {
	model.Title = "身份事件审计"
	model.AutoLoad = true
}

// ViewFieldModel 将审计字段全部设为只读，并标注常用筛选字段。
func (own *IdentityEventManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.BusinessManage.ViewFieldModel(model, field)
	field.IsEdit = false
	switch {
	case field.IsFieldOrTitle("EventID"):
		field.Title = "事件 ID"
	case field.IsFieldOrTitle("AuthType"):
		field.Title = "认证域"
		field.IsSearch = true
	case field.IsFieldOrTitle("UserID"):
		field.Title = "用户 ID"
		field.IsSearch = true
	case field.IsFieldOrTitle("EventType"):
		field.Title = "事件类型"
		field.IsSearch = true
	case field.IsFieldOrTitle("Generation"):
		field.Title = "撤销世代"
	case field.IsFieldOrTitle("Blocked"):
		field.Title = "禁止访问"
		field.IsSearch = true
	case field.IsFieldOrTitle("OccurredAt"):
		field.Title = "事件时间"
	}
}
