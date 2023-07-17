package entity

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type BaseRecordModel struct {
	types.IRecordModel `json:"-" gorm:"-"`
	*Model
	TraceID string `json:"traceid"`
}

func NewBaseRecordModel() *BaseRecordModel {
	model := &BaseRecordModel{
		Model: NewModel(),
	}
	return model
}

func (own *BaseRecordModel) AddValid() error {
	if own.TraceID == "" {
		return errors.New("traceid不能为空")
	}
	return nil
}
func (own *BaseRecordModel) UpdateValid(old interface{}) error {
	return errors.New("数据不能修改")

}
func (own *BaseRecordModel) RemoveValid() error {
	return errors.New("数据不能删除")
}

func (own *BaseRecordModel) GetHash() string {
	return utils.HashCodes(own.TraceID)
}

func (own *BaseRecordModel) Equals(o interface{}) bool {
	if own.Model != nil && own.Model.ID != 0 {
		return own.Model.Equals(o)
	}
	if ao, ok := o.(types.IRecordModel); ok {
		if own.Hashcode != "" && ao.GetHash() != "" {
			return own.Hashcode == ao.GetHash()
		}
	}
	return false
}
