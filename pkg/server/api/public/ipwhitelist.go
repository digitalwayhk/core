package public

import (
	"net/http"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
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
	return own.ServerArgs.Validation(req)
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
	row, err := list.SearchOne(func(item *pt.SearchItem) {
		item.AddWhereN("name", own.Ip)
	})
	if err != nil {
		return nil, err
	}
	if row == nil {
		row := list.NewItem()
		row.Name = own.Ip
		row.Timeout = own.Timeout
		err := list.Add(row)
		if err != nil {
			return nil, err
		}
	} else {
		row.Timeout = own.Timeout
		now := time.Now()
		row.UpdatedAt = &now
		row.CreatedAt = &now
		err := list.Update(row)
		if err != nil {
			return nil, err
		}
	}
	err = list.Save()
	if err != nil {
		return nil, err
	}
	rows, _, err := list.SearchAll(1, 1000)
	return rows, err
}

func (own *IpWhiteList) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own,
		router.WithMethod(http.MethodGet),
		withSystemEndpointRateLimit(),
	)
}
