package casdoor

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestLegacyAuthHandlerFailsClosedWithoutExplicitClient(t *testing.T) {
	called := false
	handler := AuthHandler(func(http.ResponseWriter, *http.Request) { called = true })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	require.NotPanics(t, func() { handler.ServeHTTP(response, request) })
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.False(t, called)
}

func TestTokenParseRequiresExplicitClient(t *testing.T) {
	claims, err := TokenParse("token")
	require.Nil(t, claims)
	require.ErrorIs(t, err, ErrClientRequired)
}

func TestExplicitClientAuthHandlerFailsClosedForRawCasdoorToken(t *testing.T) {
	client := &fakeClient{claims: &casdoorsdk.Claims{User: casdoorsdk.User{Id: "user-1", Name: "alice"}}}
	domain := &DomainClient{client: client, organization: "org", application: "app"}
	called := false
	handler := NewAuthHandler(domain, func(http.ResponseWriter, *http.Request) { called = true })
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	require.False(t, called)
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Equal(t, "authentication failed\n", response.Body.String())
	require.Empty(t, client.parsedToken)
}

type fakeClient struct {
	claims      *casdoorsdk.Claims
	parseErr    error
	parsedToken string
}

func (*fakeClient) GetOAuthToken(string, string, ...casdoorsdk.OAuthOption) (*oauth2.Token, error) {
	return nil, errors.New("not implemented in auth middleware test")
}

func (c *fakeClient) ParseJwtToken(token string) (*casdoorsdk.Claims, error) {
	c.parsedToken = token
	return c.claims, c.parseErr
}

func (*fakeClient) GetUser(string) (*casdoorsdk.User, error) {
	return nil, errors.New("not implemented in auth middleware test")
}
