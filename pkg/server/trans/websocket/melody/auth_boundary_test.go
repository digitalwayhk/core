package melody

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"
)

func TestLogonRejectsCasdoorTokenWhenCasdoorEnabled(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	publicKey := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})
	casdoorsdk.InitConfig("http://casdoor.invalid", "", "", string(publicKey), "", "")

	claims := &casdoorsdk.Claims{
		User: casdoorsdk.User{Id: "casdoor-user", Email: "user@example.com"},
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Second)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(privateKey)
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
