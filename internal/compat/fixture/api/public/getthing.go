package public

import (
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type GetThing struct {
	Name string `json:"name" desc:"名称"`
}

func (r *GetThing) Parse(types.IRequest) error      { return nil }
func (r *GetThing) Validation(types.IRequest) error { return nil }
func (r *GetThing) Do(types.IRequest) (interface{}, error) {
	return map[string]string{"status": "ok"}, nil
}
func (r *GetThing) GetResponse() interface{} { return map[string]string{"status": "ok"} }
func (r *GetThing) RouterInfo() *types.RouterInfo {
	info := router.DefaultRouterInfo(r)
	info.Method = http.MethodGet
	return info
}
