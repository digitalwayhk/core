package manage

import (
	"strings"

	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// CloneItem 是带表单的自定义命令：以选中行做底稿，提交后新增一条资料条目。
type CloneItem struct {
	managepkg.Operation[models.CatalogItem]
}

// NewCloneItem 创建绑定 Manage owner 的复制命令。
func NewCloneItem(instance interface{}) *CloneItem {
	return &CloneItem{Operation: managepkg.NewOperation[models.CatalogItem](instance)}
}

// New 为请求创建独立命令实例。
func (own *CloneItem) New(instance interface{}) servertypes.IRouter {
	return NewCloneItem(instance)
}

// Validation 校验复制表单中的编码、名称和分类。
func (own *CloneItem) Validation(servertypes.IRequest) error {
	if own.Model == nil {
		return models.NewValidationError("请填写要复制的资料")
	}
	own.Model.ID = 0
	own.Model.Code = strings.ToLower(strings.TrimSpace(own.Model.Code))
	own.Model.Name = strings.TrimSpace(own.Model.Name)
	if own.Model.Code == "" {
		return models.NewValidationError("条目编码不能为空")
	}
	exists, err := own.Model.CodeExists(own.Model.Code, 0)
	if err != nil {
		return err
	}
	if exists {
		own.Model.Code += "-copy"
	}
	return own.Model.AddValid()
}

// Do 把表单值作为新条目写入。
func (own *CloneItem) Do(req servertypes.IRequest) (interface{}, error) {
	list := models.NewManageModelList[models.CatalogItem]()
	own.Model.SetID(req.NewID())
	own.Model.State = 0
	if err := list.Add(own.Model); err != nil {
		return nil, err
	}
	if err := list.Save(); err != nil {
		return nil, err
	}
	if err := own.Model.SaveChildren(); err != nil {
		return nil, err
	}
	return own.Model, nil
}

// RouterInfo 注册 /api/manage/catalog/catalogitemmanage/cloneitem。
func (own *CloneItem) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
