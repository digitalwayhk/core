package manage

import (
	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// TokenManage 币种管理,用于管理币种的增删改查
type TokenManage struct {
	*manage.ManageService[models.TokenModel]
}

// NewTokenManage 新建币种管理
func NewTokenManage() *TokenManage {
	own := &TokenManage{}
	own.ManageService = manage.NewManageService[models.TokenModel](own)
	return own
}

func (own *TokenManage) ViewModel(model *view.ViewModel) {
	model.AutoLoad = true
}
