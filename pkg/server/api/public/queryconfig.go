package public

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type QueryConfig struct {
	api.ServerArgs
}

func (own *QueryConfig) Validation(req types.IRequest) error {
	ip := req.GetClientIP()
	if !req.Authorized() {
		if !utils.HasLocalIPAddr(ip) {
			return errors.New("QueryConfig接口只能在本地访问！")
		}
	}
	return nil
}
func (own *QueryConfig) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	return sc.Config, nil
}

func (own *QueryConfig) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}

func (own *QueryConfig) GetConfigPath(address string, port int) (*config.ServerConfig, error) {
	path := address + ":" + strconv.Itoa(port) + own.RouterInfo().Path
	values, err := rest.PostJson(path, nil, nil)
	if err != nil {
		return nil, err
	}
	res := &router.Response{}
	err = json.Unmarshal(values, res)
	if err != nil {
		return nil, err
	}
	if res.Success {
		con := &config.ServerConfig{}
		data, err := json.Marshal(res.Data)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(data, con)
		if err != nil {
			return nil, err
		}
		return con, nil
	} else {
		return nil, errors.New(res.ErrorMessage)
	}
}
