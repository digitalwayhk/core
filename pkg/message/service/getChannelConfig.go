package service

import (
	"errors"
	IModel "github.com/digitalwayhk/core/models"
	"github.com/digitalwayhk/core/pkg/message/client/mail"
	"github.com/digitalwayhk/core/pkg/message/client/sms"
	"github.com/digitalwayhk/core/pkg/message/models"
	"github.com/digitalwayhk/core/pkg/message/types"
	pType "github.com/digitalwayhk/core/pkg/persistence/types"
	"golang.org/x/exp/slices"
)

func GetMsgChannel(identifier string, isMail bool) (types.IMsg, error) {

	functionList := IModel.NewManageModelList[models.ChannelStatus]()
	subItem := functionList.GetSearchItem()
	subItem.AddWhereN("usable", true)
	subItem.AddWhereN("identifier", identifier)
	if !isMail {
		subItem.AddWhere(&pType.WhereItem{Relation: "OR", Column: "identifier", Value: "Global"})
	} else {
		subItem.AddWhere(&pType.WhereItem{Relation: "OR", Column: "identifier", Value: "All"})
	}
	subItem.AddSort(&pType.SortItem{Column: "identifier", IsDesc: false})
	subItem.AddSort(&pType.SortItem{Column: "priority", IsDesc: false})
	if err := functionList.LoadList(subItem); err != nil {
		return nil, errors.New("channel not found")
	}

	var cList []uint
	var list []*models.ChannelStatus
	for _, channel := range functionList.ToArray() {
		if !slices.Contains(cList, channel.ChannelId) {
			cList = append(cList, channel.ChannelId)
			list = append(list, channel)
		}
	}
	channelList := IModel.NewManageModelList[models.MsgChannel]()
	item, err := channelList.SearchId(list[0].ChannelId)
	if err != nil {
		return nil, err
	}
	return GetChannelInit(item, isMail), nil
}

func GetChannelInit(channel *models.MsgChannel, isMail bool) types.IMsg {
	if isMail {
		return mail.NewOutlookSMTP(channel)
	}

	switch channel.Name {
	case "MontNet":
		return sms.NewMontNet(channel)
	case "SmsCountry":
		return sms.NewSmsCountry(channel)
	default:
		return sms.NewMontNet(channel)
	}
}
