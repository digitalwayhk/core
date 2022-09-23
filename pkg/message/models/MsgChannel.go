package models

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
	"strings"
)

type MsgChannel struct {
	*entity.Model                  //`json:"-"`
	Name          string           `json:"name"`
	Type          int              `json:"type"`
	Host          string           `json:"host"`
	Port          uint             `json:"port"`
	User          string           `json:"user"`
	Pass          string           `json:"pass"`
	Status        []*ChannelStatus `gorm:"foreignkey:ChannelId"`
}

type ChannelStatus struct {
	*entity.Model `json:"-"`
	ChannelId     uint   `json:"channel_id"`
	Identifier    string `json:"identifier"`
	Priority      int    `json:"priority"`
	Usable        bool   `json:"usable"`
}

//func (own *ChannelConfig) GetHash() string {
//	return utils.HashCodes(own.Name)
//}

func (own *MsgChannel) GetLocalDBName() string {
	return strings.ToLower(utils.GetTypeName(own))
}

func (own *ChannelStatus) GetLocalDBName() string {
	return strings.ToLower("MsgChannel")
}

func NewMsgChannel() *MsgChannel {
	return &MsgChannel{
		Model: entity.NewModel(),
	}
}

func (own *MsgChannel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

func (own *ChannelStatus) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
