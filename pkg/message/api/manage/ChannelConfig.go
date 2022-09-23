package manage

import (
	"github.com/digitalwayhk/core/pkg/message/models"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type ChannelConfig struct {
	*manage.ManageService[models.MsgChannel]
}

func NewChannelConfig() *ChannelConfig {
	own := &ChannelConfig{}
	own.ManageService = manage.NewManageService[models.MsgChannel](own)
	return own
}

func (own *ChannelConfig) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0)
	routers = append(routers, own.ManageService.Routers()...)
	//router/ = append(routers, NewDisable(own))
	return routers
}

func (own *ChannelConfig) ViewFieldModel(model interface{}, field *view.FieldModel) {
	//field.Disabled = true
	//field.IsSearch = false
	if field.IsFieldOrTitle("Type") {
		field.ComBox("短信", "郵件", "站內信")
	}
	if _, ok := model.(*models.MsgChannel); ok {
		viewFieldMsgChannel(field)
	}
	if _, ok := model.(*models.ChannelStatus); ok {
		viewFieldChannelStatus(field)
	}
}

func viewFieldMsgChannel(field *view.FieldModel) {
	if field.IsFieldOrTitle("Pass") {
		field.IsPassword = true
	}
}

func viewFieldChannelStatus(field *view.FieldModel) {
	if field.IsFieldOrTitle("ChannelId") {
		field.Visible = false
	}
	if field.IsFieldOrTitle("Identifier") {
		field.Title = "發送識別"
	}
	if field.IsFieldOrTitle("Priority") {
		field.Title = "優先順序"
	}
	if field.IsFieldOrTitle("Usable") {
		field.Title = "是否可用"
	}
}

func (own *ChannelConfig) ViewChildModel(child *view.ViewChildModel) {
	if child.Name == "Status" {
		child.Title = "發送配置"
	}
}
