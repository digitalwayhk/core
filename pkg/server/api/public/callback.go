package public

import (
	"fmt"
	"net/http"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Callback struct {
	Code  string `json:"code" form:"code" binding:"required"`
	State string `json:"state" form:"state" binding:"required"`
}

func (own *Callback) Parse(req types.IRequest) error {
	own.Code = req.GetValue("code")
	own.State = req.GetValue("state")
	return nil
}
func (own *Callback) Validation(req types.IRequest) error {
	con := router.GetContext(req.ServiceName())
	if !con.Config.Auth.CasDoor.Enable && !con.Config.ManageAuth.CasDoor.Enable {
		return fmt.Errorf("casdoor auth is not enabled")
	}
	if own.Code == "" {
		return fmt.Errorf("code is required")
	}
	if own.State == "" {
		return fmt.Errorf("state is required")
	}
	return nil
}

func (own *Callback) Do(req types.IRequest) (interface{}, error) {
	token, err := casdoorsdk.GetOAuthToken(own.Code, own.State)
	if err != nil {
		err := fmt.Errorf("GetOAuthToken() error: %w", err)
		return nil, err
	}
	return token.AccessToken, nil
}

func (own *Callback) RouterInfo() *types.RouterInfo {
	info := router.DefaultRouterInfo(own)
	info.Method = http.MethodGet
	info.Path = "/api/callback"
	return info
}
