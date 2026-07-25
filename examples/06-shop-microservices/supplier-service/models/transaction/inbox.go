// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

// Inbox 定义本文件能力使用的核心结构。
type Inbox struct {
	*common.BusinessModel
	EventID   string `gorm:"not null;uniqueIndex"`
	EventType string `gorm:"not null;index"`
}

// NewInbox 执行本文件能力对应的业务操作。
func NewInbox() *Inbox { return &Inbox{BusinessModel: common.NewBusinessModel()} }

// NewModel 实现本类型在当前服务边界中的行为。
func (i *Inbox) NewModel() {
	if i.BusinessModel == nil || i.SupplierServiceModel == nil || i.Model == nil {
		i.BusinessModel = common.NewBusinessModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (i *Inbox) GetHash() string { return utils.HashCodes(i.EventID) }
