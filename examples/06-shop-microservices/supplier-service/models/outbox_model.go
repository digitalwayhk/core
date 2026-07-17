package models

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

// Outbox 与商品事实同事务写入，发布成功后才标记完成。
type Outbox struct {
	*common.BusinessModel
	EventID   string `gorm:"not null;uniqueIndex" json:"eventID"`
	EventType string `gorm:"not null;index" json:"eventType"`
	Subject   string `gorm:"not null" json:"subject"`
	Payload   []byte `gorm:"type:blob" json:"-"`
	Published bool   `gorm:"index" json:"published"`
}

func NewOutbox() *Outbox { return &Outbox{BusinessModel: common.NewBusinessModel()} }

func (o *Outbox) NewModel() {
	if o.BusinessModel == nil || o.SupplierServiceModel == nil || o.Model == nil {
		o.BusinessModel = common.NewBusinessModel()
	}
}

func (o *Outbox) GetHash() string { return utils.HashCodes(o.EventID) }
