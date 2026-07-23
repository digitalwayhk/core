package public

import (
	"fmt"
	"net/http"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type CasdoorConfig struct {
	Type string `json:"type" desc:"type of casdoor info, auth or manage"`
}

// Casdoor 为旧 Go 类型名保留兼容别名。
// Deprecated: 使用 CasdoorConfig。
type Casdoor = CasdoorConfig
type CasdoorResponse struct {
	Endpoint              string `yaml:"endpoint"`
	ClientID              string `yaml:"client_id"`
	Organization          string `yaml:"organization"`
	Application           string `yaml:"application"`
	BackgroundCallbackURL string `yaml:"background_callback_url"`
}

func (own *CasdoorConfig) Parse(req types.IRequest) error {
	own.Type = req.GetValue("type")
	return nil
}
func (own *CasdoorConfig) Validation(req types.IRequest) error {
	authType, err := normalizeCasdoorAuthType(own.Type)
	if err != nil {
		return err
	}
	own.Type = string(authType)
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

func (own *CasdoorConfig) Do(req types.IRequest) (interface{}, error) {
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
	casdoorRes := &CasdoorResponse{
		Endpoint:              casdoorConfig.Server.Endpoint,
		ClientID:              casdoorConfig.Server.ClientID,
		Organization:          casdoorConfig.Server.Organization,
		Application:           casdoorConfig.Server.Application,
		BackgroundCallbackURL: casdoorCallbackPath(),
	}
	return casdoorRes, nil
}
func (own *CasdoorConfig) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(own,
		router.WithMethod(http.MethodGet),
		router.WithPath("/api/casdoor"),
		withSystemEndpointRateLimit(),
	)
}
