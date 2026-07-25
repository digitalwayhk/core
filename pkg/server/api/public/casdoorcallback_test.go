package public

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestCasdoorCallbackUsesNewPath(t *testing.T) {
	require.Equal(t, "/api/casdoor/callback", (&CasdoorCallback{}).RouterInfo().GetPath())
	require.Equal(t, (&CasdoorCallback{}).RouterInfo().GetPath(), (&Callback{}).RouterInfo().GetPath())
}

func TestCasdoorCallbackRejectsForbiddenUser(t *testing.T) {
	client := &callbackClientStub{
		organization: "org",
		token:        &oauth2.Token{AccessToken: "provider-token"},
		claims:       &casdoorsdk.Claims{User: casdoorsdk.User{Name: "alice"}},
		user: &casdoorsdk.User{
			Id:          "user-1",
			Name:        "alice",
			Owner:       "org",
			IsForbidden: true,
		},
	}
	authority := &callbackAuthorityStub{}

	_, err := authenticateCasdoorCallback(
		context.Background(), authTestServiceContext(&authHookRecorder{}), client, authority,
		types.AuthTypeUser, "code", "state", time.Unix(1_900_000_000, 0).UTC(),
	)

	contract := types.ResolvePublicError(err)
	require.Equal(t, 401, contract.HTTPStatus)
	require.Equal(t, "authentication failed", contract.Message)
	require.NotContains(t, contract.Message, "IsForbidden")
	require.Zero(t, authority.confirmCalls)
}

func TestCasdoorCallbackConfirmsGenerationBeforeIssuing(t *testing.T) {
	hook := &authHookRecorder{}
	client := activeCallbackClient()
	authority := &callbackAuthorityStub{
		current:   authstate.State{Generation: 7},
		confirmed: authstate.State{Generation: 7},
	}

	pair, err := authenticateCasdoorCallback(
		context.Background(), authTestServiceContext(hook), client, authority,
		types.AuthTypeUser, "code", "state", time.Unix(1_900_000_000, 0).UTC(),
	)

	require.NoError(t, err)
	require.NotEmpty(t, pair.AccessToken)
	require.Equal(t, uint64(7), authority.expectedGeneration)
	require.Equal(t, "alice", hook.captured.Identity.ProviderSubject)
	require.Equal(t, uint64(7), hook.captured.Identity.Generation)
}

func TestCasdoorCallbackBindsNormalizedAuthorityService(t *testing.T) {
	hook := &authHookRecorder{}
	sc := authTestServiceContext(hook)
	sc.Service = &types.Service{Name: " Orders "}
	authority := &callbackAuthorityStub{
		current:   authstate.State{Generation: 7},
		confirmed: authstate.State{Generation: 7},
	}

	_, err := authenticateCasdoorCallback(
		context.Background(), sc, activeCallbackClient(), authority,
		types.AuthTypeManage, "code", "state", time.Unix(1_900_000_000, 0).UTC(),
	)

	require.NoError(t, err)
	require.Equal(t, "orders", hook.captured.Identity.AuthorityService)
}

func TestCasdoorCallbackAllowsVerifiedReloginAfterLogout(t *testing.T) {
	client := activeCallbackClient()
	authority := &callbackAuthorityStub{
		current:   authstate.State{Generation: 8, Blocked: true},
		confirmed: authstate.State{Generation: 8, Blocked: false},
	}

	pair, err := authenticateCasdoorCallback(
		context.Background(), authTestServiceContext(&authHookRecorder{}), client, authority,
		types.AuthTypeUser, "code", "state", time.Now().UTC(),
	)

	require.NoError(t, err)
	require.NotEmpty(t, pair.AccessToken)
	require.Equal(t, 1, authority.confirmCalls)
	require.Equal(t, uint64(8), authority.expectedGeneration)
}

func TestCasdoorCallbackRejectsConcurrentGenerationChange(t *testing.T) {
	client := activeCallbackClient()
	authority := &callbackAuthorityStub{
		current:    authstate.State{Generation: 7},
		confirmErr: authstate.ErrGenerationChanged,
	}

	pair, err := authenticateCasdoorCallback(
		context.Background(), authTestServiceContext(&authHookRecorder{}), client, authority,
		types.AuthTypeUser, "code", "state", time.Now().UTC(),
	)

	require.Error(t, err)
	require.Empty(t, pair.AccessToken)
	require.Equal(t, 401, types.ResolvePublicError(err).HTTPStatus)
}

func TestCasdoorCallbackKeepsTypedHookErrorSafe(t *testing.T) {
	hook := &authHookRecorder{reject: types.NewPublicError(
		types.ErrorKindForbidden, 40321, "账户已冻结", errors.New("secret account state"),
	)}
	client := activeCallbackClient()
	authority := &callbackAuthorityStub{
		current:   authstate.State{Generation: 2},
		confirmed: authstate.State{Generation: 2},
	}

	pair, err := authenticateCasdoorCallback(
		context.Background(), authTestServiceContext(hook), client, authority,
		types.AuthTypeUser, "code", "state", time.Now().UTC(),
	)

	contract := types.ResolvePublicError(err)
	require.Empty(t, pair.AccessToken)
	require.Equal(t, 403, contract.HTTPStatus)
	require.Equal(t, "账户已冻结", contract.Message)
	require.NotContains(t, contract.Message, "secret")
}

type callbackClientStub struct {
	organization string
	token        *oauth2.Token
	claims       *casdoorsdk.Claims
	user         *casdoorsdk.User
	err          error
	getUserName  string
}

func (c *callbackClientStub) Organization() string { return c.organization }
func (c *callbackClientStub) GetOAuthToken(string, string, ...casdoorsdk.OAuthOption) (*oauth2.Token, error) {
	return c.token, c.err
}
func (c *callbackClientStub) ParseJwtToken(string) (*casdoorsdk.Claims, error) {
	return c.claims, c.err
}
func (c *callbackClientStub) GetUser(name string) (*casdoorsdk.User, error) {
	c.getUserName = name
	return c.user, c.err
}

type callbackAuthorityStub struct {
	current            authstate.State
	currentErr         error
	confirmed          authstate.State
	confirmErr         error
	confirmCalls       int
	expectedGeneration uint64
}

func (a *callbackAuthorityStub) Current(context.Context, types.AuthIdentity) (authstate.State, error) {
	return a.current, a.currentErr
}
func (a *callbackAuthorityStub) ConfirmActive(_ context.Context, _ types.AuthIdentity, generation uint64) (authstate.State, error) {
	a.confirmCalls++
	a.expectedGeneration = generation
	return a.confirmed, a.confirmErr
}
func activeCallbackClient() *callbackClientStub {
	return &callbackClientStub{
		organization: "org",
		token:        &oauth2.Token{AccessToken: "provider-token"},
		claims:       &casdoorsdk.Claims{User: casdoorsdk.User{Name: "alice"}},
		user: &casdoorsdk.User{
			Id:          "user-1",
			Name:        "alice",
			DisplayName: "用户一",
			Owner:       "org",
		},
	}
}
