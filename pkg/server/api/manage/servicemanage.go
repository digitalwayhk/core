package manage

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ServiceInfo 是管理界面展示的当前服务运行快照。
type ServiceInfo struct {
	*entity.Model
	ServiceName  string
	Address      string
	Port         int
	LogType      string
	MaxConns     int
	MaxBytes     int64
	Timeout      int64
	CpuThreshold int64
}

func (own *ServiceInfo) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

type ServiceManage struct {
	*manage.ManageService[ServiceInfo]
}

func NewServiceManage() *ServiceManage {
	own := &ServiceManage{}
	own.ManageService = manage.NewManageService[ServiceInfo](own)
	return own
}

func (own *ServiceManage) Routers() []types.IRouter {
	return []types.IRouter{own.ManageService.View, own.ManageService.Search}
}

func (own *ServiceManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	field.Disabled = true
	field.IsSearch = false
	if _, ok := model.(*ServiceInfo); !ok {
		return
	}
	switch {
	case field.IsFieldOrTitle("ServiceName"):
		field.Title = "服务名称"
	case field.IsFieldOrTitle("Address"):
		field.Title = "运行地址"
	case field.IsFieldOrTitle("Port"):
		field.Title = "运行端口"
	case field.IsFieldOrTitle("LogType"):
		field.Title = "日志类型"
	case field.IsFieldOrTitle("MaxConns"):
		field.Title = "最大请求数"
	case field.IsFieldOrTitle("MaxBytes"):
		field.Title = "最大请求字节数"
	case field.IsFieldOrTitle("Timeout"):
		field.Title = "请求超时(ms)"
	case field.IsFieldOrTitle("CpuThreshold"):
		field.Title = "限流cpu阈值"
	case field.IsFieldOrTitle("CreatedAt"), field.IsFieldOrTitle("UpdatedAt"):
		field.Visible = false
	}
}

func (own *ServiceManage) SearchBefore(_ interface{}, req types.IRequest) (interface{}, error, bool) {
	list := make([]*ServiceInfo, 0)
	for name, context := range router.GetContexts() {
		if name == "persistence" || name == "server" {
			continue
		}
		info := &ServiceInfo{
			Model:        entity.NewModel(),
			ServiceName:  name,
			Address:      context.RuntimeAddress(),
			Port:         context.Config.Port,
			LogType:      context.Config.Log.Mode,
			MaxConns:     context.Config.MaxConns,
			MaxBytes:     context.Config.MaxBytes,
			Timeout:      context.Config.Timeout,
			CpuThreshold: context.Config.CpuThreshold,
		}
		info.ID = req.NewID()
		list = append(list, info)
	}
	return &view.TableData{Rows: list, Total: int64(len(list))}, nil, true
}
