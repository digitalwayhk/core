package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Inbox struct {
	*common.BusinessModel
	EventID   string `gorm:"not null;uniqueIndex"`
	EventType string `gorm:"not null;index"`
	Processed bool   `gorm:"index"`
}

func NewInbox() *Inbox { return &Inbox{BusinessModel: common.NewBusinessModel()} }

func (i *Inbox) NewModel() {
	if i.BusinessModel == nil || i.UserServiceModel == nil || i.Model == nil {
		i.BusinessModel = common.NewBusinessModel()
	}
}

func (i *Inbox) GetHash() string { return utils.HashCodes(i.EventID) }
