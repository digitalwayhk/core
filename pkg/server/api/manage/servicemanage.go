package manage

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"

	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type ServiceInfo struct {
	*entity.Model
	ServiceName     string
	RunIp           string
	Port            int
	LogType         string
	MaxConns        int   //最大请求数
	MaxBytes        int64 //最大请求字节数
	Timeout         int64 //超时时间(ms)
	CpuThreshold    int64 //cpu阈值
	Attachs         []*AttachInfo
	CallRouters     []*CallRouterInfo
	ObserverRouters []*ObserverRouterInfo
}
type AttachInfo struct {
	*entity.Model
	ServiceName       string
	AttachServiceName string
	Address           string
	Port              int
	IsAttach          bool
}
type CallRouterInfo struct {
	*entity.Model
	ServiceName string
	RouterPath  string
	RouterType  types.ApiType
}
type ObserverRouterInfo struct {
	*entity.Model
	ServiceName  string
	RouterPath   string
	ObserverType int
	IsOk         bool
	IsUnSub      bool
}

func test(ms manage.IManageSearch) {

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
	routers := make([]types.IRouter, 0)
	routers = append(routers, own.ManageService.View)
	routers = append(routers, own.ManageService.Search)
	routers = append(routers, own.ManageService.Edit)
	return routers
}
func (own *ServiceManage) ViewCommandModel(cmd *view.CommandModel) {
	if cmd.Command == "edit" {
		cmd.Name = "设置依赖服务地址"
	}
}
func (own *ServiceManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	field.Disabled = true
	field.IsSearch = false
	if _, ok := model.(*ServiceInfo); ok {
		viewfieldServiceInfo(field)
	}
	if _, ok := model.(*AttachInfo); ok {
		viewfieldAttachInfo(field)
	}
	if field.IsFieldOrTitle("CreatedAt") || field.IsFieldOrTitle("UpdatedAt") {
		field.Visible = false
	}
}
func viewfieldServiceInfo(field *view.FieldModel) {
	if field.IsFieldOrTitle("ServiceName") {
		field.Title = "服务名称"
	}
	if field.IsFieldOrTitle("RunIp") {
		field.Title = "运行IP"
	}
	if field.IsFieldOrTitle("Port") {
		field.Title = "运行端口"
	}
	if field.IsFieldOrTitle("LogType") {
		field.Title = "日志类型"
	}
	if field.IsFieldOrTitle("MaxConns") {
		field.Title = "最大请求数"
	}
	if field.IsFieldOrTitle("MaxBytes") {
		field.Title = "最大请求字节数"
	}
	if field.IsFieldOrTitle("Timeout") {
		field.Title = "请求超时(ms)"
	}
	if field.IsFieldOrTitle("CpuThreshold") {
		field.Title = "限流cpu阈值"
	}
}
func viewfieldAttachInfo(field *view.FieldModel) {
	if field.IsFieldOrTitle("ServiceName") {
		field.Visible = false
	}
	if field.IsFieldOrTitle("AttachServiceName") {
		field.Title = "依赖服务名称"
	}
	if field.IsFieldOrTitle("Address") {
		field.Title = "地址"
	}
	if field.IsFieldOrTitle("Port") {
		field.Title = "端口"
	}
	if field.IsFieldOrTitle("IsAttach") {
		field.Title = "是否附加"
	}
	if field.IsFieldOrTitle("address") || field.IsFieldOrTitle("port") {
		field.Disabled = false
	}
}
func viewfieldCallRouteInfo(field *view.FieldModel) {
	if field.IsFieldOrTitle("ServiceName") {
		field.Title = "依赖服务名称"
	}
	if field.IsFieldOrTitle("RouterPath") {
		field.Title = "路由路径"
	}
	if field.IsFieldOrTitle("RouterType") {
		field.Title = "路由类型"
	}
}
func viewfieldObserverRouteInfo(field *view.FieldModel) {
	if field.IsFieldOrTitle("ServiceName") {
		field.Title = "依赖服务名称"
	}
	if field.IsFieldOrTitle("RouterPath") {
		field.Title = "订阅路径"
	}
	if field.IsFieldOrTitle("ObserverType") {
		field.Title = "订阅类型"
		field.ComBox("请求发生时", "请求完成时", "异常发生时")
	}
	if field.IsFieldOrTitle("IsOk") {
		field.Title = "是否可用"
	}
	if field.IsFieldOrTitle("IsUnSub") {
		field.Disabled = false
		field.Title = "取消订阅"
	}
}
func (own *ServiceManage) ViewChildModel(child *view.ViewChildModel) {
	child.IsAdd = false
	child.IsRemove = false
	if child.Name == "Attachs" {
		child.Title = "依赖服务信息"
	}
	if child.Name == "CallRouters" {
		child.Title = "调用路由信息"
	}
	if child.Name == "ObserverRouters" {
		child.Title = "订阅路由信息"
	}
}
func (own *ServiceManage) SearchBefore(sender interface{}, req types.IRequest) (interface{}, error, bool) {
	list := make([]*ServiceInfo, 0)
	for name, ctext := range router.GetContexts() {
		if name == "persistence" || name == "server" {
			continue
		}
		info := &ServiceInfo{
			Model:           entity.NewModel(),
			ServiceName:     name,
			RunIp:           ctext.Config.RunIp,
			Port:            ctext.Config.Port,
			LogType:         ctext.Config.Log.Mode,
			MaxConns:        ctext.Config.MaxConns,
			MaxBytes:        ctext.Config.MaxBytes,
			Timeout:         ctext.Config.Timeout,
			CpuThreshold:    ctext.Config.CpuThreshold,
			Attachs:         make([]*AttachInfo, 0),
			CallRouters:     make([]*CallRouterInfo, 0),
			ObserverRouters: make([]*ObserverRouterInfo, 0),
		}
		info.ID = req.NewID()
		for an, atts := range ctext.Service.AttachService {
			item := &AttachInfo{
				Model:             entity.NewModel(),
				ServiceName:       name,
				AttachServiceName: an,
				IsAttach:          atts.IsAttach,
			}
			con := ctext.Config.AttachServices[an]
			if con != nil {
				item.Address = con.Address
				item.Port = con.Port
			}
			item.ID = req.NewID()
		}
		list = append(list, info)
	}
	data := &view.TableData{
		Rows:  list,
		Total: int64(len(list)),
	}
	return data, nil, true
}
func (own *ServiceManage) ChildSearchBefore(sender interface{}, req types.IRequest) (interface{}, error, bool) {
	if search, ok := sender.(*manage.Search[ServiceInfo]); ok {
		if info, ok := search.SearchItem.Parent.(*ServiceInfo); ok {
			sc := router.GetContext(info.ServiceName)
			if sc == nil {
				return nil, errors.New(info.ServiceName + "未找到！"), true
			}
			return getchilddata(sc, req, search.SearchItem.ChildModel), nil, true
		}
		return nil, nil, true
	}
	return nil, nil, false
}
func getchilddata(sc *router.ServiceContext, req types.IRequest, cm *view.ViewChildModel) interface{} {
	attachs := make([]*AttachInfo, 0)
	callrou := make([]*CallRouterInfo, 0)
	obrou := make([]*ObserverRouterInfo, 0)
	for an, atts := range sc.Service.AttachService {
		item := &AttachInfo{
			Model:             entity.NewModel(),
			ServiceName:       sc.Config.Name,
			AttachServiceName: an,
			IsAttach:          atts.IsAttach,
		}
		con := sc.Config.AttachServices[an]
		if con != nil {
			item.Address = con.Address
			item.Port = con.Port
		}
		item.ID = req.NewID()
		attachs = append(attachs, item)
		for c, rou := range atts.CallRouters {
			ri := rou.RouterInfo()
			cr := &CallRouterInfo{
				Model:       entity.NewModel(),
				ServiceName: an,
				RouterPath:  c,
				RouterType:  ri.PathType,
			}
			callrou = append(callrou, cr)
		}
		for o, rou := range atts.ObserverRouters {
			or := &ObserverRouterInfo{
				Model:        entity.NewModel(),
				ServiceName:  rou.ServiceName,
				RouterPath:   o,
				IsOk:         rou.IsOk,
				ObserverType: int(rou.State),
			}
			obrou = append(obrou, or)
		}
	}
	if cm == nil || cm.Name == "Attachs" {
		return &view.TableData{
			Rows:  attachs,
			Total: int64(len(attachs)),
		}
	}
	if cm.Name == "CallRouters" {
		return &view.TableData{
			Rows:  callrou,
			Total: int64(len(callrou)),
		}
	}
	if cm.Name == "ObserverRouters" {
		return &view.TableData{
			Rows:  obrou,
			Total: int64(len(obrou)),
		}
	}
	return nil
}
func (own *ServiceManage) ValidationBefore(sender interface{}, req types.IRequest) (error, bool) {
	if edit, ok := sender.(*manage.Edit[ServiceInfo]); ok {
		for _, item := range edit.Model.Attachs {
			if item.Address == "" || item.Port == 0 {
				return errors.New(item.AttachServiceName + " 附加服务的地址或端口为空，不能设置服务地址"), true
			}
		}
		return nil, true
	}
	return nil, false
}
func (own *ServiceManage) DoBefore(sender interface{}, req types.IRequest) (interface{}, error, bool) {
	if edit, ok := sender.(*manage.Edit[ServiceInfo]); ok {
		for _, item := range edit.Model.Attachs {
			ctext := router.GetContext(edit.Model.ServiceName)
			caa := ctext.Config.AttachServices[item.AttachServiceName]
			if caa == nil {
				caa = &config.AttachAddress{
					Name: item.AttachServiceName,
				}
				ctext.Config.AttachServices[item.AttachServiceName] = caa
			}
			caa.Address = item.Address
			caa.Port = item.Port
			err := ctext.Config.Save()
			if err != nil {
				return nil, err, true
			}
			err = ctext.SetAttachServiceAddress(item.AttachServiceName)
			if err != nil {
				return nil, err, true
			}
			if as, ok := ctext.Service.AttachService[item.AttachServiceName]; ok {
				for _, row := range edit.Model.ObserverRouters {
					if or, ok := as.ObserverRouters[row.RouterPath]; ok {
						or.IsUnSub = row.IsUnSub
					}
				}
			}
			err = ctext.RegisterObserve(&public.Observe{})
			if err != nil {
				return nil, err, true
			}
		}
		return nil, nil, true
	}
	return nil, nil, false
}
