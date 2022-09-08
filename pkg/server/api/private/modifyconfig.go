package private

import (
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type ModifyConfig struct {
	api.ServerArgs
	Config *config.ServerConfig
}

func (own *ModifyConfig) Parse(req types.IRequest) error {
	return req.Bind(own.Config)
}
func (own *ModifyConfig) Do(req types.IRequest) (interface{}, error) {
	con := router.GetContext(req.ServiceName()).Config
	con = own.Config
	err := con.Save()
	if err != nil {
		return nil, err
	}
	return nil, err
}

func (own *ModifyConfig) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
