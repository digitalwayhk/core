// Package projection 定义 07 供应商服务本地订单投影模型。
package projection

import (
	"errors"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// SupplierOrder 保存订单事件在供应商服务形成的本地投影。
type SupplierOrder struct {
	*common.ServiceBaseModel
	OrderID       uint   `gorm:"not null;uniqueIndex" json:"orderID"`
	SupplierID    uint   `gorm:"not null;index" json:"supplierID"`
	ProductID     uint   `gorm:"not null;index" json:"productID"`
	UserID        uint   `gorm:"not null;index" json:"userID"`
	Quantity      int    `json:"quantity"`
	OrderStatus   string `json:"orderStatus"`
	PaymentStatus string `json:"paymentStatus"`
}

// NewSupplierOrder 创建供应商订单投影模型。
func NewSupplierOrder() *SupplierOrder {
	return &SupplierOrder{ServiceBaseModel: common.NewServiceBaseModel()}
}

// NewModel 初始化持久化框架需要的嵌入模型。
func (o *SupplierOrder) NewModel() {
	if o.ServiceBaseModel == nil || o.Model == nil {
		o.ServiceBaseModel = common.NewServiceBaseModel()
	}
}

// GetHash 返回供应商订单投影业务唯一散列。
func (o *SupplierOrder) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(o.OrderID), 10))
}

// InsertWith 将供应商订单投影写入指定事务。
func (o *SupplierOrder) InsertWith(action persistencetypes.IDataAction) error {
	if o.OrderID == 0 || o.SupplierID == 0 || o.ProductID == 0 || o.UserID == 0 {
		return errors.New("供应商订单投影参数不完整")
	}
	o.SetHashcode(o.GetHash())
	return action.Insert(o)
}

// UpdateWith 更新指定事务中的供应商订单投影。
func (o *SupplierOrder) UpdateWith(action persistencetypes.IDataAction) error {
	o.SetUpdatedAt(time.Now().UTC())
	o.SetHashcode(o.GetHash())
	return action.Update(o)
}
