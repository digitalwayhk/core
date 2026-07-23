// Package basedata 定义 07 供应商服务基础资料模型。
package basedata

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// Supplier 保存供应商用户在后台维护的供应商资料。
type Supplier struct {
	*common.ServiceBaseModel
	UserID      uint   `gorm:"not null;index" json:"userID"`
	Code        string `gorm:"not null;uniqueIndex" json:"code"`
	Name        string `gorm:"not null" json:"name"`
	Description string `json:"description"`
	Enabled     bool   `gorm:"index" json:"enabled"`
}

// NewSupplier 创建供应商模型。
func NewSupplier() *Supplier {
	return &Supplier{ServiceBaseModel: common.NewServiceBaseModel(), Enabled: true}
}

// NewModel 初始化持久化框架需要的嵌入模型。
func (s *Supplier) NewModel() {
	if s.ServiceBaseModel == nil || s.Model == nil {
		s.ServiceBaseModel = common.NewServiceBaseModel()
	}
}

// GetHash 返回供应商业务唯一散列。
func (s *Supplier) GetHash() string { return utils.HashCodes(strings.TrimSpace(s.Code)) }

// InsertWith 将供应商写入指定事务。
func (s *Supplier) InsertWith(action persistencetypes.IDataAction) error {
	if s.UserID == 0 || strings.TrimSpace(s.Code) == "" || strings.TrimSpace(s.Name) == "" {
		return errors.New("供应商参数不完整")
	}
	s.SetHashcode(s.GetHash())
	return action.Insert(s)
}

// UpdateWith 更新指定事务中的供应商。
func (s *Supplier) UpdateWith(action persistencetypes.IDataAction) error {
	s.SetUpdatedAt(time.Now().UTC())
	s.SetHashcode(s.GetHash())
	return action.Update(s)
}
