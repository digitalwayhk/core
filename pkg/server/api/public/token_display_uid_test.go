// 本文件验证展示 UID 经过真实签发和刷新后，仍使用原始 Casdoor 身份校验。
package public

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// TestCasdoorDisplayUIDSurvivesRefreshAndRevocation 验证登录/刷新/旧令牌升级与撤销代次检查。
func TestCasdoorDisplayUIDSurvivesRefreshAndRevocation(t *testing.T) {
	now := time.Unix(1_900_000_000, 0).UTC()
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "新短ID令牌", true: "旧UUID令牌刷新"}[legacy], func(t *testing.T) {
			hook := &authHookRecorder{mutate: func(args *types.AuthHookArgs) {
				require.Equal(t, "user-1", args.UID)
				require.Equal(t, "user-1", args.Identity.UID)
				require.NoError(t, args.Claims.(types.ITokenUIDMutator).SetTokenUID("1234567"))
			}}
			sc := authTestServiceContext(hook)
			if legacy {
				sc.AuthHookProvider = &authHookRecorder{}
			}
			client := activeCallbackClient()
			authority := &callbackAuthorityStub{current: authstate.State{Generation: 4}, confirmed: authstate.State{Generation: 4}}
			pair, err := authenticateCasdoorCallback(context.Background(), sc, client, authority, types.AuthTypeUser, "code", "state", now)
			require.NoError(t, err)
			sc.AuthHookProvider = hook
			refreshed, err := refreshForServiceWithDependenciesAt(context.Background(), sc, pair.RefreshToken, types.AuthTypeUser, now.Add(time.Hour), client, authority)
			require.NoError(t, err)
			access, err := safe.ValidateAccessToken(refreshed.AccessToken, sc.Config.Auth.AccessSecret, types.AuthTypeUser, now.Add(time.Hour))
			require.NoError(t, err)
			require.Equal(t, "1234567", access.Claims["uid"])
			require.Equal(t, "user-1", access.Identity.UID)
			authority.current.Generation++
			_, err = refreshForServiceWithDependenciesAt(context.Background(), sc, pair.RefreshToken, types.AuthTypeUser, now.Add(time.Hour), client, authority)
			require.Equal(t, 401, types.ResolvePublicError(err).HTTPStatus)
		})
	}
}
