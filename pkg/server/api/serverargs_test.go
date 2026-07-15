package api_test

import (
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/api"
	publicapi "github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/stretchr/testify/require"
)

func TestServerRouterInfoAppliesRegistrationOptions(t *testing.T) {
	info := api.ServerRouterInfo(
		&publicapi.TestToken{},
		router.WithMethod(http.MethodPost),
	)

	require.Equal(t, "/api/servermanage/testtoken", info.GetPath())
	require.Equal(t, http.MethodPost, info.GetMethod())
}
