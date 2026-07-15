package melody

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func TestLogonRejectsCasdoorTokenWhenCasdoorEnabled(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "casdoor-user",
		"iat": time.Now().Add(-time.Second).Unix(),
		"exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString(privateKey)
	require.NoError(t, err)

	subscriptions := &SessionSubscriptions{
		manage: &MelodyManager{serviceContext: &router.ServiceContext{
			Config: &config.ServerConfig{Auth: config.AuthSecret{
				AccessSecret: "internal-access-secret",
				CasDoor:      config.CasDoorConfig{Enable: true},
			}},
		}},
	}

	err = subscriptions.Logon(&SessionRequest{Token: token})
	require.Error(t, err)
	require.Nil(t, subscriptions.req)
}
