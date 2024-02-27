package smodels

import "github.com/digitalwayhk/core/pkg/persistence/entity"

type IPWhiteModel struct {
	*entity.Model
	Name    string
	Timeout int64
}

func (own *IPWhiteModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
func (own *IPWhiteModel) GetLocalDBName() string {
	return "ipwhitemodels"
}
