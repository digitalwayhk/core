package entity

import (
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Model struct {
	ID         uint      `gorm:"primarykey" json:"id,string"`
	CreatedAt  time.Time `json:"createdat"`
	UpdatedAt  time.Time `json:"updatedat"`
	ModelState int       `gorm:"-" json:"modelState"` // 0: normal, 1: add, 2: update, 3: remove
	Hashcode   string    `json:"-"`
}

func NewModel() *Model {
	model := &Model{
		ModelState: 0,
	}
	return model
}

func (own *Model) NewModel() {

}
func (own *Model) Equals(o interface{}) bool {
	ao, ok := o.(types.IModel)
	if !ok {
		return false
	}
	if iro, ok := o.(types.IRowCode); ok {
		if iro.GetHash() != "" && own.GetHash() != "" {
			return iro.GetHash() == own.GetHash()
		}
	}
	if ao.GetID() > 0 && own.GetID() > 0 {
		return ao.GetID() == own.GetID()
	}
	return false
}
func (own *Model) GetID() uint {
	return own.ID
}
func (own *Model) GetModelState() int {
	return own.ModelState
}
func (own *Model) SetModelState(state int) {
	own.ModelState = state
}
func (own *Model) GetHash() string {
	return utils.HashCodes(strconv.Itoa(int(own.ID)))
}

func (own *Model) SearchWhere(ws []*types.WhereItem) ([]*types.WhereItem, error) {
	return ws, nil
}
func (own *Model) AddValid() error {
	return nil
}
func (own *Model) UpdateValid(old interface{}) error {
	return nil
}
func (own *Model) RemoveValid() error {
	return nil
}
func (own *Model) GetLocalDBName() string {
	return "models"
}

func (own *Model) GetRemoteDBName() string {
	return "models"
}

// 弃用，无法通过own获取子struct
func (own *Model) GetField(field string) interface{} {
	return utils.GetPropertyValue(own, field)
}

// 弃用，无法通过own获取子struct
func (own *Model) SetField(field string, value interface{}) error {
	if field == "Hashcode" {
		own.Hashcode = value.(string)
	}
	return utils.SetPropertyValue(own, field, value)
}
func (own *Model) SetHashcode(code string) {
	own.Hashcode = code
}

// func (own *Model) MarshalJSON() ([]byte, error) {
// 	type Alias Model
// 	return json.Marshal(&struct {
// 		*Alias
// 		DeletedAt gorm.DeletedAt `json:"-"`
// 	}{
// 		Alias:     (*Alias)(own),
// 		DeletedAt: gorm.DeletedAt{},
// 	})
// }
// func (own *Model) UnmarshalJSON(b []byte) error {
// 	type Alias Model
// 	aux := &struct {
// 		*Alias
// 		DeletedAt gorm.DeletedAt `json:"-"`
// 	}{
// 		Alias:     (*Alias)(own),
// 		DeletedAt: gorm.DeletedAt{},
// 	}
// 	if err := json.Unmarshal(b, &aux); err != nil {
// 		return err
// 	}
// 	return nil
// }
