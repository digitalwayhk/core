package basedata

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

type PaymentType struct {
	*common.BaseDataModel
	Name    string `gorm:"not null" json:"name"`
	Code    string `gorm:"not null;uniqueIndex" json:"code"`
	Enabled bool   `json:"enabled"`
}

func NewPaymentType() *PaymentType {
	return &PaymentType{BaseDataModel: common.NewBaseDataModel(), Enabled: false}
}

func (p *PaymentType) NewModel() {
	if p.BaseDataModel == nil || p.OrderServiceModel == nil || p.Model == nil {
		p.BaseDataModel = common.NewBaseDataModel()
	}
}

func (p *PaymentType) GetHash() string {
	return utils.HashCodes(strings.ToLower(strings.TrimSpace(p.Code)))
}
