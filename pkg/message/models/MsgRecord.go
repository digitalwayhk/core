package models

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"go.mongodb.org/mongo-driver/bson"
	"time"
)

type MsgRecord struct {
	*entity.BaseRecordModel `bson:",inline"`
	*MsgDetails             `json:",inline" bson:",inline"`
}

type MsgDetails struct {
	UserId   uint   `json:"userId" bson:"userId"`
	Username string `json:"userName" bson:"userName"`
	//RequestIp     string `json:"requestIp" bson:"requestIp"`
	Type    int    `json:"type" bson:"type"`
	Device  string `json:"device" bson:"device"`
	Title   string `json:"title" bson:"title"`
	Content string `json:"content" bson:"content"`
	Channel string `json:"channel" bson:"channel"`
}

func (own *MsgRecord) NewModel() {
	if own.BaseRecordModel == nil {
		own.BaseRecordModel = entity.NewBaseRecordModel()
	}
	if own.MsgDetails == nil {
		own.MsgDetails = NewMsgDetails()
	}
}

func NewMsgDetails() *MsgDetails {
	return &MsgDetails{}
}

func (own *MsgRecord) GetRemoteDBName() string {
	return "zb"
}

// MarshalBSON 自動填充 updated_at, created_at
func (own *MsgRecord) MarshalBSON() ([]byte, error) {
	if own.CreatedAt.IsZero() {
		own.CreatedAt = time.Now()
	}
	own.UpdatedAt = time.Now()

	type my MsgRecord
	return bson.Marshal((*my)(own))
}

func (own MsgRecord) GetField(field string) interface{} {

	return nil
}
