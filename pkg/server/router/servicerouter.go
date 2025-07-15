package router

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type GetRouterPath interface {
	GetRouterPathPrefixes() string
}

type ServiceRouter struct {
	Service              *ServiceContext
	publicAPI            map[string]*types.RouterInfo
	privateAPI           map[string]*types.RouterInfo
	manageAPI            map[string]*types.RouterInfo
	serverManagerAPI     map[string]*types.RouterInfo
	allAPI               map[string]*types.RouterInfo
	IsCloseServerManager bool
}

func DefaultRouterInfo(item interface{}) *types.RouterInfo {
	pack, name := GetRouterPackAndTypeName(item)
	index := strings.Index(pack, "api")
	if index <= 1 {
		panic(fmt.Sprintf("api package not found,Path:%s  \n 请修改包名称或手动指定路由路径。", pack))
	}
	return NewRouterInfo(item, pack, name)
}
func NewServiceRouter(service *ServiceContext, tis types.IService) *ServiceRouter {
	sr := &ServiceRouter{
		Service:          service,
		publicAPI:        make(map[string]*types.RouterInfo),
		privateAPI:       make(map[string]*types.RouterInfo),
		manageAPI:        make(map[string]*types.RouterInfo),
		serverManagerAPI: make(map[string]*types.RouterInfo),
		allAPI:           make(map[string]*types.RouterInfo),
	}
	if csm, ok := tis.(types.ICloseServerManage); ok {
		sr.IsCloseServerManager = csm.IsCloseServerManage()
	}
	sr.AddRoutes(service.Service.Routers...)
	return sr
}
func (own *ServiceRouter) AddRoutes(routers ...types.IRouter) {
	for _, router := range routers {
		info := router.RouterInfo()
		if info.ServiceName != own.Service.Service.Name {
			tsn := strings.ToLower(info.ServiceName)
			ssn := strings.ToLower(own.Service.Service.Name)
			info.Path = strings.Replace(info.Path, tsn, ssn, -1)
			info.ServiceName = own.Service.Service.Name
		}
		if info.PathType == types.PublicType {
			own.publicAPI[info.Path] = info
		}
		if info.PathType == types.PrivateType {
			own.privateAPI[info.Path] = info
		}
		if info.PathType == types.ManageType {
			own.manageAPI[info.Path] = info
		}
		if _, ok := own.allAPI[info.Path]; !ok {
			own.allAPI[info.Path] = info
		} else {
			panic(fmt.Sprintf("service :%s router already exists. path:%s", info.ServiceName, info.Path))
		}
	}
}
func (own *ServiceRouter) AddServerRouters(routers ...types.IRouter) {
	for _, router := range routers {
		info := router.RouterInfo()
		own.serverManagerAPI[info.Path] = info
	}
}
func (own *ServiceRouter) GetRouters() []*types.RouterInfo {
	var routes []*types.RouterInfo
	for _, v := range own.allAPI {
		routes = append(routes, v)
	}
	if !own.IsCloseServerManager {
		for _, v := range own.serverManagerAPI {
			routes = append(routes, v)
		}
	}
	return routes
}
func (own *ServiceRouter) GetTypeRouters(t types.ApiType) []*types.RouterInfo {
	var routes []*types.RouterInfo
	var API map[string]*types.RouterInfo
	switch t {
	case types.PublicType:
		API = own.publicAPI
	case types.PrivateType:
		API = own.privateAPI
	case types.ManageType:
		API = own.manageAPI
	case types.ServerManagerType:
		API = own.serverManagerAPI
	}
	for _, v := range API {
		routes = append(routes, v)
	}
	return routes
}
func (own *ServiceRouter) GetRouter(path string) *types.RouterInfo {
	if v, ok := own.allAPI[path]; ok {
		return v
	}
	if v, ok := own.serverManagerAPI[path]; ok {
		return v
	}
	return nil
}
func (own *ServiceRouter) HasRouter(path string, t types.ApiType) bool {
	res := false
	switch t {
	case types.PublicType:
		_, res = own.publicAPI[path]
	case types.PrivateType:
		_, res = own.privateAPI[path]
	case types.ManageType:
		_, res = own.manageAPI[path]
	case types.ServerManagerType:
		_, res = own.serverManagerAPI[path]
	}
	return res
}
func GetRouterPackAndTypeName(item interface{}) (string, string) {
	pack := utils.GetPackageName(item)
	name := ""
	if ioh, ok := item.(types.IPackRouterHook); ok {
		pack = utils.GetPackageName(ioh.GetInstance())
		name = utils.GetTypeName(ioh.GetInstance())
	}
	if name == "" {
		name = utils.GetTypeName(item)
	}
	return pack, name
}
func NewRouterInfo(item interface{}, pack, name string) *types.RouterInfo {
	index := strings.Index(pack, "api")
	Pathtype := types.ApiType(pack[index+4:])
	servicepack := pack[:index-1]
	index = strings.LastIndex(servicepack, "/")
	servername := strings.ToLower(servicepack[index+1:])
	auth := false
	if Pathtype == types.PrivateType {
		auth = true
	}
	Path := "/api/" + strings.ToLower(servername) + "/" + strings.ToLower(name)
	if item, ok := item.(GetRouterPath); ok {
		Path = item.GetRouterPathPrefixes() + "/" + strings.ToLower(name)
	}
	if !config.INITSERVER {
		sc := GetContext(servername)
		if sc != nil {
			info := sc.Router.GetRouter(Path)
			if info != nil {
				return info
			}
		}
	}
	sname := utils.GetTypeName(item)
	info := &types.RouterInfo{
		ID:                utils.HashCode(Path),
		Path:              Path,
		Auth:              auth,
		PackPath:          pack,
		Method:            http.MethodPost,
		ServiceName:       servername,
		PathType:          Pathtype,
		InstanceName:      name,
		StructName:        sname,
		Subscriber:        make(map[types.ObserveState]map[string]*types.ObserveArgs, 3),
		WebSocketWaitTime: time.Second * 10,
	}
	info.SetInstance(item.(types.IRouter))
	return info
}
