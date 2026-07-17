package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Outbox struct {
	*common.BusinessModel
	EventID   string `gorm:"not null;uniqueIndex"`
	EventType string `gorm:"not null;index"`
	Subject   string `gorm:"not null"`
	Payload   []byte `gorm:"type:blob"`
	Published bool   `gorm:"index"`
}

func NewOutbox() *Outbox { return &Outbox{BusinessModel: common.NewBusinessModel()} }

func (o *Outbox) NewModel() {
	if o.BusinessModel == nil || o.OrderServiceModel == nil || o.Model == nil {
		o.BusinessModel = common.NewBusinessModel()
	}
}

func (o *Outbox) GetHash() string { return utils.HashCodes(o.EventID) }
