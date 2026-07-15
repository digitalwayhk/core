package api_test

import (
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/api"
	publicapi "github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

var _ func(interface{}) *types.RouterInfo = api.ServerRouterInfo

func TestServerRouterInfoAppliesRegistrationOptions(t *testing.T) {
	info := api.ServerRouterInfoWithOptions(
		&publicapi.TestToken{},
		router.WithMethod(http.MethodPost),
	)

	require.Equal(t, "/api/servermanage/testtoken", info.GetPath())
	require.Equal(t, http.MethodPost, info.GetMethod())
}
