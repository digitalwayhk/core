package button

import (
	"fmt"

	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
)

// ExportData 自定义操作按钮示例，演示如何为管理页面添加非标准 CRUD 操作。
//
// 继承 manage.Operation[T] 获得框架路由注册能力；
// 在 Routers() 中追加 button.NewExportData[T](own) 即可注册到管理页面。
type ExportData[T pt.IModel] struct {
	manage.Operation[T]
}

func NewExportData[T pt.IModel](instance interface{}) *ExportData[T] {
	return &ExportData[T]{
		Operation: manage.NewOperation[T](instance),
	}
}

func (own *ExportData[T]) New(instance interface{}) st.IRouter {
	if own.GetInstance() == nil {
		own.Operation.New(instance)
	}
	return own
}

// Parse 跳过 Body 绑定：导出按钮无请求体，避免经过 Nginx 反代时读到 EOF。
func (own *ExportData[T]) Parse(req st.IRequest) error {
	return nil
}

func (own *ExportData[T]) Validation(req st.IRequest) error {
	return own.Operation.Validation(req)
}

// Do 执行导出逻辑，返回下载链接或文件内容。
func (own *ExportData[T]) Do(req st.IRequest) (interface{}, error) {
	// 实际项目：查询数据、生成 CSV/Excel 并返回下载地址
	return fmt.Sprintf("export ok"), nil
}

// RouterInfo 使用框架快捷方法，自动设置路径、鉴权类型为 ManageType。
func (own *ExportData[T]) RouterInfo() *st.RouterInfo {
	return manage.RouterInfo(own)
}
