package models

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Supplier struct {
	*common.BaseDataModel
	AuthUserID  string `gorm:"not null;uniqueIndex" json:"-"`
	Name        string `gorm:"not null;uniqueIndex" json:"name"`
	Code        string `gorm:"not null;uniqueIndex" json:"code"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

func NewSupplier() *Supplier { return &Supplier{BaseDataModel: common.NewBaseDataModel()} }

func (s *Supplier) NewModel() {
	if s.BaseDataModel == nil || s.SupplierServiceModel == nil || s.Model == nil {
		s.BaseDataModel = common.NewBaseDataModel()
	}
}

func (s *Supplier) GetHash() string {
	return utils.HashCodes(strings.ToLower(strings.TrimSpace(s.AuthUserID)))
}
