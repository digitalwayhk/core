// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

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

// NewOutbox 执行本文件能力对应的业务操作。
func NewOutbox() *Outbox { return &Outbox{BusinessModel: common.NewBusinessModel()} }

// NewModel 实现本类型在当前服务边界中的行为。
func (o *Outbox) NewModel() {
	if o.BusinessModel == nil || o.SupplierServiceModel == nil || o.Model == nil {
		o.BusinessModel = common.NewBusinessModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (o *Outbox) GetHash() string { return utils.HashCodes(o.EventID) }
