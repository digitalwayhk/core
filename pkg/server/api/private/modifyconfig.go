package private

import (
	"errors"
	"strings"

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
	// Bind into a fresh value. Binding into the nil pointer field leaves the
	// request dependent on decoder-specific allocation behavior.
	own.Config = &config.ServerConfig{}
	return req.Bind(own.Config)
}

func (own *ModifyConfig) Validation(req types.IRequest) error {
	if err := own.ServerArgs.Validation(req); err != nil {
		return err
	}
	if own.Config == nil {
		return errors.New("配置不能为空")
	}
	context := router.GetContext(req.ServiceName())
	if context == nil || context.Config == nil {
		return errors.New("目标服务不存在")
	}
	// The route service name is authoritative; never allow a body to redirect
	// a write to another service's etc file.
	if strings.TrimSpace(own.Config.Name) != "" && own.Config.Name != context.Config.Name {
		return errors.New("配置服务名称与目标服务不一致")
	}
	own.Config.Name = context.Config.Name
	return nil
}
func (own *ModifyConfig) Do(req types.IRequest) (interface{}, error) {
	context := router.GetContext(req.ServiceName())
	if context == nil || context.Config == nil {
		return nil, errors.New("目标服务不存在")
	}
	merged, err := config.MergeProtectedFields(context.Config, own.Config)
	if err != nil {
		return nil, err
	}
	merged.Name = context.Config.Name
	// Save and publish the same object. The previous implementation only
	// reassigned a local variable, so runtime requests kept using stale config.
	if err := merged.Save(); err != nil {
		return nil, err
	}
	context.Config = merged
	return merged, nil
}

func (own *ModifyConfig) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfo(own)
}
