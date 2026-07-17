package basedata

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Address struct {
	*common.BaseDataModel
	UserID    uint   `gorm:"not null;index" json:"userID"`
	Recipient string `gorm:"not null" json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}

func NewAddress() *Address { return &Address{BaseDataModel: common.NewBaseDataModel()} }

func (a *Address) NewModel() {
	if a.BaseDataModel == nil || a.UserServiceModel == nil || a.Model == nil {
		a.BaseDataModel = common.NewBaseDataModel()
	}
}

func (a *Address) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(a.UserID), 10), strings.TrimSpace(a.Recipient), strings.TrimSpace(a.Phone), strings.TrimSpace(a.Detail))
}
