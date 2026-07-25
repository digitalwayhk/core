package router_test

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
)

func TestWithServiceNameAppliesBeforeRegistrationFreeze(t *testing.T) {
	info := router.NewRouterInfoWithOptions(&optionServiceRoute{}, "example/service/api/public", "Lookup",
		router.WithServiceName("stable-service"), router.WithPath("/api/stable-service/lookup"))
	assert.Equal(t, "stable-service", info.GetServiceName())
	assert.Equal(t, "/api/stable-service/lookup", info.GetPath())
}

type optionServiceRoute struct{}

func (*optionServiceRoute) Parse(types.IRequest) error             { return nil }
func (*optionServiceRoute) Validation(types.IRequest) error        { return nil }
func (*optionServiceRoute) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (*optionServiceRoute) GetResponse() interface{}               { return nil }
func (r *optionServiceRoute) RouterInfo() *types.RouterInfo        { return router.DefaultRouterInfo(r) }
