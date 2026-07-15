package public

import (
	"fmt"
	"net/http"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Callback struct {
	Code  string `json:"code" form:"code" binding:"required"`
	State string `json:"state" form:"state" binding:"required"`
	Type  string `json:"type" desc:"type of casdoor config, auth or manage"`
}

func (own *Callback) Parse(req types.IRequest) error {
	own.Code = req.GetValue("code")
	own.State = req.GetValue("state")
	own.Type = req.GetValue("type")
	return nil
}
func (own *Callback) Validation(req types.IRequest) error {
	if own.Type == "" {
		own.Type = "auth"
	}
	if own.Type != string(types.AuthTypeUser) && own.Type != string(types.AuthTypeManage) {
		return fmt.Errorf("casdoor auth type is invalid")
	}
	con := router.GetContext(req.ServiceName())
	if own.Type == "manage" {
		if !con.Config.ManageAuth.CasDoor.Enable {
			return fmt.Errorf("casdoor manage auth is not enabled")
		}
	}
	if own.Type == "auth" {
		if !con.Config.Auth.CasDoor.Enable {
			return fmt.Errorf("casdoor auth is not enabled")
		}
	}
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
	con := router.GetContext(req.ServiceName())
	var data *config.CasDoorConfigData
	var err error
	if own.Type == "manage" {
		data, err = con.Config.ManageAuth.CasDoor.GetConfigData()
	}
	if own.Type == "auth" {
		data, err = con.Config.Auth.CasDoor.GetConfigData()
	}
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, fmt.Errorf("casdoor config data is nil")
	}
	token, err := casdoorsdk.GetOAuthToken(own.Code, own.State)
	if err != nil {
		return nil, fmt.Errorf("GetOAuthToken() error: %w", err)
	}
	claims, err := casdoorsdk.ParseJwtToken(token.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("ParseJwtToken() error: %w", err)
	}
	return issueForService(
		requestContext(req),
		con,
		claims.Id,
		claims.Email,
		types.AuthType(own.Type),
		types.AuthSourceCallback,
		claims,
	)
}

func (*Callback) GetResponse() interface{} {
	return &safe.TokenPairResponse{}
}

func (own *Callback) RouterInfo() *types.RouterInfo {
	info := router.DefaultRouterInfo(own)
	info.Method = http.MethodGet
	info.Path = "/api/callback"
	return info
}
