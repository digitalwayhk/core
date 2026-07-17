package models

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Inbox struct {
	*common.BusinessModel
	EventID   string `gorm:"not null;uniqueIndex"`
	EventType string `gorm:"not null;index"`
}

func NewInbox() *Inbox { return &Inbox{BusinessModel: common.NewBusinessModel()} }

func (i *Inbox) NewModel() {
	if i.BusinessModel == nil || i.SupplierServiceModel == nil || i.Model == nil {
		i.BusinessModel = common.NewBusinessModel()
	}
}

func (i *Inbox) GetHash() string { return utils.HashCodes(i.EventID) }
