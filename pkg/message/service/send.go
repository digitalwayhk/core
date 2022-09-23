package service

import (
	"errors"
	"fmt"
	"github.com/digitalwayhk/core/pkg/message/models"
	"github.com/digitalwayhk/core/pkg/message/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"log"
)

type NotifyInfo struct {
	UserId     uint
	Username   string
	TemplateId uint
	TraceID    string
	Data       interface{}
}

func NewNotifyInfo(username, traceId string) *NotifyInfo {
	return &NotifyInfo{
		Username: username,
		TraceID:  traceId,
	}
}

func PreSend(info *NotifyInfo) error {
	if !utils.IsEmail(info.Username) && !utils.IsMobile(info.Username) {
		return errors.New("device is neither an email or mobile")
	}
	templateService := GetTemplateService()
	if templateService.tList.Count() == 0 {
		templateService.reloadTemplate()
	}
	if templateService.cList.Count() == 0 {
		templateService.reloadContent()
	}
	temp, err := templateService.GetTemplate(info.TemplateId)
	if err != nil {
		return err
	}
	tempType := types.SmsMsg
	if utils.IsEmail(info.Username) {
		tempType = types.MailMsg
	}
	cont, err := templateService.GetContent(temp.ID, tempType)
	sendLog := fmt.Sprintf(info.Username+"_%v", info.Data)
	log.Println(temp.Title, sendLog)
	return TestingEnvSend(info, tempType, info.Username, temp.Title, fmt.Sprintf(cont.Details, info.Data))
}

func TestingEnvSend(info *NotifyInfo, msgType int, device, title, content string) error {
	record := models.NewMsgDetails()
	record.Username = info.Username
	record.UserId = info.UserId
	record.Type = msgType
	record.Device = device
	record.Title = title
	record.Content = content
	if err := AddMsgRecord(info.TraceID, record); err != nil {
		log.Println("Login Record save error: ", err)
	}
	return nil
}

func Send(device, title, content string) error {
	var msgAdapter *MsgAdapter
	if utils.IsEmail(device) {
		msgAdapter = NewMsgAdapter(true)
	} else {
		msgAdapter = NewMsgAdapter(false)
	}
	channel, err := msgAdapter.GetChannel(device)
	if err != nil {
		return errors.New("get msg channel error: " + err.Error())
	}
	details := types.NewSendDetail()
	details.Device.Text = device
	details.Title.Text = title
	details.Content.Text = content

	_, err = channel.SendMsg(details)
	if err != nil {
		return errors.New("msg send error: " + err.Error())
	}

	return nil
}
