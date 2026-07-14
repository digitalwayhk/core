package manage

import (
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ShopManage 是继承商城全部 Manage 的服务级公共层。
// owner 必须始终是最终具体 Manage，确保框架 hook 分派正确。
type ShopManage[T persistencetypes.IModel] struct {
	*managepkg.ManageService[T]
}

// NewShopManage 创建绑定最终 owner 的服务级 Manage。
func NewShopManage[T persistencetypes.IModel](owner interface{}) *ShopManage[T] {
	return &ShopManage[T]{ManageService: managepkg.NewManageService[T](owner)}
}

// ParseAfter 保留服务级输入规范化扩展点。
func (own *ShopManage[T]) ParseAfter(interface{}, servertypes.IRequest) error { return nil }

// ValidationAfter 保留服务级校验扩展点。
func (own *ShopManage[T]) ValidationAfter(interface{}, servertypes.IRequest) error { return nil }

// ViewFieldModel 配置框架公共字段。
func (own *ShopManage[T]) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("ID") || field.IsFieldOrTitle("CreatedAt") || field.IsFieldOrTitle("UpdatedAt") {
		field.IsEdit = false
	}
}

// ViewChildModel 保留服务级子表配置扩展点。
func (own *ShopManage[T]) ViewChildModel(*view.ViewChildModel) {}
