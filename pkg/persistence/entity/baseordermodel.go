package entity

import (
	"errors"
	"strconv"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

//BaseOrderModel 基础单据模型
type BaseOrderModel struct {
	types.IOrderModel `json:"-" gorm:"-"`
	*Model
	TraceID string `json:"-"`
	Code    string `json:"code"`
	UserID  uint   `json:"userid,string"`
	State   int    `json:"state"`
}

func NewBaseOrderModel() *BaseOrderModel {
	model := &BaseOrderModel{
		Model: NewModel(),
	}
	return model
}
func (own *BaseOrderModel) NewModel() {
	if own.Model == nil {
		own.Model = NewModel()
	}
}

func (own *BaseOrderModel) AddValid() error {
	if own.TraceID == "" {
		return errors.New("traceid不能为空")
	}
	if own.UserID == 0 {
		return errors.New("userid不能为空")
	}
	return nil
}
func (own *BaseOrderModel) UpdateValid(old interface{}) error {
	if own.TraceID == "" {
		return errors.New("traceid不能为空")
	}
	if own.UserID == 0 {
		return errors.New("userid不能为空")
	}
	return nil
}
func (own *BaseOrderModel) RemoveValid() error {
	return errors.New("单据不能删除")
}
func (own *BaseOrderModel) GetHash() string {
	return utils.HashCodes(own.TraceID, strconv.Itoa(int(own.UserID)))
}
