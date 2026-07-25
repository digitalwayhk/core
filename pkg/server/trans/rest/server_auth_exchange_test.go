package rest

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/require"
)

func TestCasdoorModeUsesInternalJWT(t *testing.T) {
	auth := config.AuthSecret{
		AccessSecret: "internal-access-secret",
		CasDoor:      config.CasDoorConfig{Enable: true},
	}

	require.Equal(t, authModeInternalJWT, selectAuthMode(auth))
}
