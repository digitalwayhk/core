package sms

import (
	"github.com/digitalwayhk/core/pkg/message/models"
	"github.com/digitalwayhk/core/pkg/message/types"
	"log"
)

type SmsCountry struct {
	Host string
	Port uint
	User string
	Pass string
}

func NewSmsCountry(channel *models.MsgChannel) *SmsCountry {
	return &SmsCountry{
		Port: channel.Port,
		Host: channel.Host,
		User: channel.User,
		Pass: channel.Pass,
	}
}

func (s SmsCountry) SendMsg(detail *types.SendDetail) (bool, error) {
	//TODO implement me
	log.Println("Sending with SmsCountry")

	return false, nil
}
