package entity

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// BaseModel 基础模型
type BaseModel struct {
	types.IBaseModel `json:"-" gorm:"-"`
	*Model
	Code            string `json:"code"`
	Name            string `json:"name"`
	State           int    `json:"state"`
	Describe        string `json:"describe"`
	CreatedUser     uint   `json:"-"`
	UpdatedUser     uint   `json:"-"`
	CreatedUserName string `json:"createdusername"`
	UpdatedUserName string `json:"updatedusername"`
	TraceID         string `json:"-"`
	//ReleaseRecord   []*BaseModelReleaseRecord `gorm:"foreignkey:SourceID"`
}

func NewBaseModel() *BaseModel {
	model := &BaseModel{
		Model: NewModel(),
	}
	return model
}
func (own *BaseModel) NewModel() {
	if own.Model == nil {
		own.Model = NewModel()
	}
}
func (own *BaseModel) GetHash() string {
	return utils.HashCodes(own.Code)
}

func (own *BaseModel) AddValid() error {
	if own.ID == 0 {
		return errors.New("id不能为空")
	}
	if own.Code == "" {
		return errors.New("code不能为空")
	}
	return nil
}
func (own *BaseModel) UpdateValid(old interface{}) error {
	if own.Code == "" {
		return errors.New("code不能为空")
	}
	return nil
}
func (own *BaseModel) RemoveValid() error {
	if own.State > 0 {
		return errors.New("当前状态不能删除")
	}
	return nil
}
func (own *BaseModel) IsBaseModel() bool {
	return true
}
func (own *BaseModel) SetCode(code string) {
	own.Code = code
}
func (own *BaseModel) GetCode() string {
	return own.Code
}
func (own *BaseModel) Equals(o interface{}) bool {
	if own.Model != nil && own.Model.ID != 0 {
		return own.Model.Equals(o)
	}
	if ao, ok := o.(types.IBaseModel); ok {
		if own.Code != "" && ao.GetCode() != "" {
			return own.Code == ao.GetCode()
		}
	}
	return false
}
func (own *BaseModel) GetID() uint {
	if own.Model == nil {
		own.Model = NewModel()
	}
	return own.Model.ID
}
