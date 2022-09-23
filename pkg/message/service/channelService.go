package service

import (
	"errors"
	IModel "github.com/digitalwayhk/core/models"
	"github.com/digitalwayhk/core/pkg/message/models"
	"log"
)

var channelService *ChannelService

type ChannelService struct {
	mList *IModel.ModelList[models.MsgChannel]
	cList *IModel.ModelList[models.ChannelStatus]
}

func init() {
	channelService = &ChannelService{
		mList: IModel.NewManageModelList[models.MsgChannel](),
		cList: IModel.NewManageModelList[models.ChannelStatus](),
	}
}

func GetChannelService() *ChannelService {
	return channelService
}

func (own *ChannelService) ReloadChannel() {
	cItem := own.cList.GetSearchItem()
	cItem.AddWhereN("IsDisable", false)
	err := own.cList.LoadList(cItem)
	if err != nil {
		log.Println("MsgChannel load error: ", err)
	}
}

func (own *ChannelService) ReloadStatus() {
	cItem := own.cList.GetSearchItem()
	cItem.AddWhereN("IsDisable", false)
	err := own.cList.LoadList(cItem)
	if err != nil {
		log.Println("MsgChannel load error: ", err)
	}
}

func (own *ChannelService) GetChannel(id uint) (*models.MsgChannel, error) {
	if id == 0 {
		return nil, errors.New("id is zero")
	}
	_, cm := own.mList.FindOne(func(o *models.MsgChannel) bool {
		return o.ID == id
	})
	return cm, nil
}
func (own *ChannelService) GetStatus(channelId uint) (*models.ChannelStatus, error) {
	if channelId == 0 {
		return nil, errors.New("id is zero")
	}
	_, cm := own.cList.FindOne(func(o *models.ChannelStatus) bool {
		return o.ChannelId == channelId
	})
	return cm, nil
}
