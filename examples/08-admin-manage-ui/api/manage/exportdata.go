package manage

import (
	"github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// ExportData 把导出按钮注册进资料条目 schema。实际导出由管理界面按当前筛选在浏览器生成 Excel。
type ExportData struct {
	managepkg.Operation[models.CatalogItem]
}

// NewExportData 创建绑定 Manage owner 的导出命令。
func NewExportData(instance interface{}) *ExportData {
	return &ExportData{Operation: managepkg.NewOperation[models.CatalogItem](instance)}
}

// New 为请求创建独立命令实例。
func (own *ExportData) New(instance interface{}) servertypes.IRouter {
	return NewExportData(instance)
}

// Do 不执行服务端导出，前端按当前查询结果下载。
func (*ExportData) Do(servertypes.IRequest) (interface{}, error) {
	return map[string]string{"message": "请使用管理界面导出"}, nil
}

// RouterInfo 注册 /api/manage/catalog/catalogitemmanage/exportdata。
func (own *ExportData) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
