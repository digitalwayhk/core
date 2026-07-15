package router

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type requestSecurityService struct{ name string }

func (s *requestSecurityService) ServiceName() string                  { return s.name }
func (*requestSecurityService) Routers() []types.IRouter               { return nil }
func (*requestSecurityService) SubscribeRouters() []*types.ObserveArgs { return nil }

func TestRequestIgnoresCasdoorUserContext(t *testing.T) {
	name := fmt.Sprintf("request-casdoor-boundary-%d", time.Now().UnixNano())
	service := &requestSecurityService{name: name}
	cfg := config.NewServiceDefaultConfig(name, 31994)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal = ""
	cfg.Transport.Fallback = nil
	cfg.Auth.CasDoor.Enable = true
	cfg.Auth.CasDoor.WebhookSecret = "request-security-test-webhook"
	sc := NewServiceContextWithConfig(service, cfg)
	sc.SetRunState(true)
	t.Cleanup(func() { sc.SetRunState(false) })

	httpRequest := httptest.NewRequest("GET", "/private", nil)
	httpRequest = httpRequest.WithContext(context.WithValue(
		httpRequest.Context(),
		"user",
		casdoorsdk.User{Id: "bypass-user", Email: "bypass@example.com"},
	))
	req := &Request{auth: true, service: sc}

	uid, username := getUserIDAndName(req, httpRequest)
	require.Empty(t, uid)
	require.Empty(t, username)
}
