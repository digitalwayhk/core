package private

import (
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type CreateThing struct {
	Name string `json:"name" desc:"名称"`
}

func (r *CreateThing) Parse(types.IRequest) error      { return nil }
func (r *CreateThing) Validation(types.IRequest) error { return nil }
func (r *CreateThing) Do(types.IRequest) (interface{}, error) {
	return map[string]string{"status": "created"}, nil
}
func (r *CreateThing) GetResponse() interface{}      { return map[string]string{"status": "created"} }
func (r *CreateThing) RouterInfo() *types.RouterInfo { return router.DefaultRouterInfo(r) }
