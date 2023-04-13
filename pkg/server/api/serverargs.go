package api

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type ServerArgs struct {
	ServiceName string `json:"name"`
}

func (own *ServerArgs) Parse(req types.IRequest) error {
	own.ServiceName = req.GetValue("name")
	return nil
}
func (own *ServerArgs) Validation(req types.IRequest) error {
	ip := req.GetClientIP()
	index := strings.Index(ip, ":")
	if index > 0 {
		ip = ip[:index]
	}
	if !req.Authorized() {
		context := router.GetContext(req.ServiceName())
		if context != nil {
			opt := context.GetServerOption()
			if opt != nil && opt.RemoteAccessManageAPI {
				return nil
			}
		}
		if !utils.HasLocalIPAddr(ip) {
			return errors.New("服务管理接口只能在本地机访问！")
		}
	}
	return nil
}

func ServerRouterInfo(item interface{}) *types.RouterInfo {
	pack, name := router.GetRouterPackAndTypeName(item)
	info := router.NewRouterInfo(item, pack, name)
	info.Path = "/api/servermanage/" + strings.ToLower(info.StructName)
	return info
}
