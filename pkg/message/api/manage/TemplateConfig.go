package manage

import (
	"github.com/digitalwayhk/core/pkg/message/models"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type TemplateConfig struct {
	*manage.ManageService[models.MsgTemplate]
}

func NewTemplateConfig() *TemplateConfig {
	own := &TemplateConfig{}
	own.ManageService = manage.NewManageService[models.MsgTemplate](own)
	return own
}

func (own *TemplateConfig) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0)
	routers = append(routers, own.ManageService.Routers()...)
	//router/ = append(routers, NewDisable(own))
	return routers
}

func (own *TemplateConfig) ViewFieldModel(model interface{}, field *view.FieldModel) {
	//field.Disabled = true
	//field.IsSearch = false
	if field.IsFieldOrTitle("Type") {
		field.ComBox("短信", "郵件", "站內信")
	}
	if _, ok := model.(*models.MsgTemplate); ok {
		viewFieldMsgTemplate(field)
	}
	if _, ok := model.(*models.MsgContent); ok {
		viewFieldMsgContent(field)
	}
}

func viewFieldMsgTemplate(field *view.FieldModel) {
	if field.IsFieldOrTitle("Code") {
		field.Disabled = true
	}
	if field.IsFieldOrTitle("Pass") {
		field.IsPassword = true
	}
}

func viewFieldMsgContent(field *view.FieldModel) {
	if field.IsFieldOrTitle("TemplateId") {
		field.Visible = false
	}
	if field.IsFieldOrTitle("Type") {
		field.Title = "發送類型"
	}
	if field.IsFieldOrTitle("Details") {
		field.Title = "發送內容"
	}
	if field.IsFieldOrTitle("FilePath") {
		field.Title = "讀取模板路徑"
	}
	if field.IsFieldOrTitle("IsSend") {
		field.Title = "是否發送此類型消息"
	}
}

func (own *TemplateConfig) ViewChildModel(child *view.ViewChildModel) {
	if child.Name == "Content" {
		child.Title = "內容配置"
	}
}
