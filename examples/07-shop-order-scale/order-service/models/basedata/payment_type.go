// Package basedata 定义 07 订单服务支付类型基础资料模型。
package basedata

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// PaymentType 保存共享远程权威库中的支付方式配置。
type PaymentType struct {
	*common.ServiceBaseModel
	Name    string `gorm:"not null" json:"name"`
	Code    string `gorm:"type:varchar(191);not null;uniqueIndex" json:"code"`
	Enabled bool   `gorm:"index" json:"enabled"`
}

// NewPaymentType 创建支付类型模型。
func NewPaymentType() *PaymentType {
	return &PaymentType{ServiceBaseModel: common.NewServiceBaseModel(), Enabled: true}
}

// NewModel 初始化持久化框架需要的嵌入模型。
func (p *PaymentType) NewModel() {
	if p.ServiceBaseModel == nil || p.Model == nil {
		p.ServiceBaseModel = common.NewServiceBaseModel()
	}
}

// GetHash 返回支付类型的业务唯一散列。
func (p *PaymentType) GetHash() string { return utils.HashCodes(strings.TrimSpace(p.Code)) }

// InsertWith 将支付类型写入指定事务。
func (p *PaymentType) InsertWith(action persistencetypes.IDataAction) error {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Code) == "" {
		return errors.New("支付类型参数不完整")
	}
	p.SetHashcode(p.GetHash())
	return action.Insert(p)
}

// UpdateWith 更新指定事务中的支付类型。
func (p *PaymentType) UpdateWith(action persistencetypes.IDataAction) error {
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Code) == "" {
		return errors.New("支付类型参数不完整")
	}
	p.SetUpdatedAt(time.Now().UTC())
	p.SetHashcode(p.GetHash())
	return action.Update(p)
}
