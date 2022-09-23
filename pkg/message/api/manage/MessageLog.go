package manage

import (
	"github.com/digitalwayhk/core/models"
	msgModel "github.com/digitalwayhk/core/pkg/message/models"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type MessageLog struct {
	*manage.ManageService[msgModel.MsgRecord]
}

func NewMessageLog() *MessageLog {
	own := &MessageLog{}
	own.ManageService = manage.NewManageService[msgModel.MsgRecord](own)
	return own
}

func (own *MessageLog) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0)
	routers = append(routers, own.ManageService.Routers()...)
	return routers
}

func (own *MessageLog) ViewFieldModel(model interface{}, field *view.FieldModel) {
	field.Disabled = true
	field.IsEdit = false
	if field.IsFieldOrTitle("Type") {
		field.ComBox("短信", "郵件", "站內信")
	}
	if _, ok := model.(*msgModel.MsgRecord); ok {
		viewFieldMsgRecord(field)
	}
}

func (own *MessageLog) DoBefore(sender interface{}, req types.IRequest) (interface{}, error, bool) {

	return nil, nil, true
}

func (own *MessageLog) GetList() *models.ModelList[msgModel.MsgRecord] {
	return models.NewMongoModelList[msgModel.MsgRecord]()
}

func viewFieldMsgRecord(field *view.FieldModel) {
	if field.IsFieldOrTitle("Pass") {
		field.IsPassword = true
	}
}
