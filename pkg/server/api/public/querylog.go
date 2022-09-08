package public

import (
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type QueryLog struct {
	api.ServerArgs
}

func (own *QueryLog) Parse(req types.IRequest) error {
	return nil
}
func (own *QueryLog) Do(req types.IRequest) (interface{}, error) {

	return nil, nil
}
func (own *QueryLog) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
