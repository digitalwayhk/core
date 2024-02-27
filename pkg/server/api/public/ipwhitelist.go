package public

import (
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type IpWhiteList struct {
	api.ServerArgs
	Ip      string `json:"ip" desc:"白名单ip"`
	Timeout int64  `json:"timeout" desc:"白名单超时时间，毫秒单位,当为0时默认1小时"`
}

func (own *IpWhiteList) Parse(req types.IRequest) error {
	req.Bind(own)
	if own.Ip == "" {
		own.Ip = req.GetValue("ip")
	}
	if own.Timeout == 0 {
		out, _ := strconv.Atoi(req.GetValue("timeout"))
		own.Timeout = int64(out)
	}
	return nil
}
func (own *IpWhiteList) Validation(req types.IRequest) error {
	// ip := req.GetClientIP()
	// index := strings.Index(ip, ":")
	// if index > 0 {
	// 	ip = ip[:index]
	// }
	// if !req.Authorized() {
	// 	context := router.GetContext(req.ServiceName())
	// 	if context != nil {
	// 		opt := context.GetServerOption()
	// 		if opt != nil && opt.RemoteAccessManageAPI {
	// 			return nil
	// 		}
	// 	}
	// 	if !utils.HasLocalIPAddr(ip) {
	// 		return errors.New("服务管理接口只能在本地机访问！")
	// 	}
	// }
	// if own.Ip == "" {
	// 	return errors.New("ip不能为空!/r/n")
	// }

	return nil
}
func (own *IpWhiteList) Do(req types.IRequest) (interface{}, error) {
	list := entity.NewModelList[smodels.IPWhiteModel](nil)
	if own.Ip == "" {
		rows, _, err := list.SearchAll(1, 1000)
		return rows, err
	}
	if own.Timeout <= 0 {
		own.Timeout = int64(time.Hour)
	} else {
		own.Timeout = own.Timeout * int64(time.Minute)
	}
	row := list.NewItem()
	row.Name = own.Ip
	row.Timeout = own.Timeout
	err := list.Add(row)
	if err != nil {
		return nil, err
	}
	err = list.Save()
	if err != nil {
		return nil, err
	}
	rows, _, err := list.SearchAll(1, 1000)
	return rows, err
}

func (own *IpWhiteList) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
