// 本文件定义当前服务基础资料模型及其持久化能力。
package basedata

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

// PaymentType 定义本文件能力使用的核心结构。
type PaymentType struct {
	*common.BaseDataModel
	Name    string `gorm:"not null" json:"name"`
	Code    string `gorm:"not null;uniqueIndex" json:"code"`
	Enabled bool   `json:"enabled"`
}

// NewPaymentType 执行本文件能力对应的业务操作。
func NewPaymentType() *PaymentType {
	return &PaymentType{BaseDataModel: common.NewBaseDataModel(), Enabled: false}
}

// NewModel 实现本类型在当前服务边界中的行为。
func (p *PaymentType) NewModel() {
	if p.BaseDataModel == nil || p.OrderServiceModel == nil || p.Model == nil {
		p.BaseDataModel = common.NewBaseDataModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (p *PaymentType) GetHash() string {
	return utils.HashCodes(strings.ToLower(strings.TrimSpace(p.Code)))
}
