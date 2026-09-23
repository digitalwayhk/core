package manage

import (
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// ImportData 把导入按钮注册进资料条目 schema。实际导入由管理界面逐行调用 add。
type ImportData struct {
	managepkg.Operation[models.CatalogItem]
}

// NewImportData 创建绑定 Manage owner 的导入命令。
func NewImportData(instance interface{}) *ImportData {
	return &ImportData{Operation: managepkg.NewOperation[models.CatalogItem](instance)}
}

// New 为请求创建独立命令实例。
func (own *ImportData) New(instance interface{}) servertypes.IRouter {
	return NewImportData(instance)
}

// Do 不执行服务端导入，前端用 Excel 解析后逐行 add。
func (*ImportData) Do(servertypes.IRequest) (interface{}, error) {
	return map[string]string{"message": "请使用管理界面导入"}, nil
}

// RouterInfo 注册 /api/manage/catalog/catalogitemmanage/importdata。
func (own *ImportData) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
