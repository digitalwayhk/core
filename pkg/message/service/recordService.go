package service

import (
	"github.com/digitalwayhk/core/models"
	msgModel "github.com/digitalwayhk/core/pkg/message/models"
)

func AddMsgRecord(traceId string, mDetail *msgModel.MsgDetails) error {
	msgRec := models.NewMongoModelList[msgModel.MsgRecord]()
	record := msgRec.NewItem()

	record.TraceID = traceId
	record.MsgDetails = mDetail

	err := msgRec.Add(record)
	if err != nil {
		return err
	}
	saveErr := msgRec.Save()
	if saveErr != nil {
		return saveErr
	}
	return nil
}
