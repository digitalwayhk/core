package public

import (
	"fmt"
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Casdoor struct {
	Type string `json:"type" desc:"type of casdoor info, auth or manage"`
}
type CasdoorResponse struct {
	Endpoint              string `yaml:"endpoint"`
	ClientID              string `yaml:"client_id"`
	Organization          string `yaml:"organization"`
	Application           string `yaml:"application"`
	BackgroundCallbackURL string `yaml:"background_callback_url"`
}

func (own *Casdoor) Parse(req types.IRequest) error {
	own.Type = req.GetValue("type")
	return nil
}
func (own *Casdoor) Validation(req types.IRequest) error {
	if own.Type == "" {
		own.Type = "auth"
	}
	con := router.GetContext(req.ServiceName())
	if own.Type == "auth" {
		if !con.Config.Auth.CasDoor.Enable {
			return fmt.Errorf("casdoor auth is not enabled")
		}
	}
	if own.Type == "manage" {
		if !con.Config.ManageAuth.CasDoor.Enable {
			return fmt.Errorf("casdoor manage auth is not enabled")
		}
	}
	if !con.Config.Auth.CasDoor.Enable && !con.Config.ManageAuth.CasDoor.Enable {
		return fmt.Errorf("casdoor auth is not enabled")
	}
	return nil
}

func (own *Casdoor) Do(req types.IRequest) (interface{}, error) {
	con := router.GetContext(req.ServiceName())
	var casdoorConfig *config.CasDoorConfigData
	var err error
	if own.Type == "auth" {
		casdoorConfig, err = con.Config.Auth.CasDoor.GetConfigData()
	}
	if own.Type == "manage" {
		casdoorConfig, err = con.Config.ManageAuth.CasDoor.GetConfigData()
	}
	if err != nil {
		return nil, err
	}
	if casdoorConfig == nil {
		return nil, fmt.Errorf("casdoor %s config is nil", own.Type)
	}
	url := Callback{}
	casdoorRes := &CasdoorResponse{
		Endpoint:              casdoorConfig.Server.Endpoint,
		ClientID:              casdoorConfig.Server.ClientID,
		Organization:          casdoorConfig.Server.Organization,
		Application:           casdoorConfig.Server.Application,
		BackgroundCallbackURL: url.RouterInfo().Path,
	}
	return casdoorRes, nil
}
func (own *Casdoor) RouterInfo() *types.RouterInfo {
	info := router.DefaultRouterInfo(own)
	info.Method = http.MethodGet
	info.Path = "/api/casdoor"
	return info
}
