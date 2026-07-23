// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

// Outbox 定义本文件能力使用的核心结构。
type Outbox struct {
	*common.BusinessModel
	EventID   string `gorm:"not null;uniqueIndex"`
	EventType string `gorm:"not null;index"`
	Subject   string `gorm:"not null"`
	Payload   []byte `gorm:"type:blob"`
	Published bool   `gorm:"index"`
}

// NewOutbox 执行本文件能力对应的业务操作。
func NewOutbox() *Outbox { return &Outbox{BusinessModel: common.NewBusinessModel()} }

// NewModel 实现本类型在当前服务边界中的行为。
func (o *Outbox) NewModel() {
	if o.BusinessModel == nil || o.OrderServiceModel == nil || o.Model == nil {
		o.BusinessModel = common.NewBusinessModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (o *Outbox) GetHash() string { return utils.HashCodes(o.EventID) }
