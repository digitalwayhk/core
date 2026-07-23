package integration

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccessTokenFromData(t *testing.T) {
	t.Run("解析结构化响应", func(t *testing.T) {
		data, err := json.Marshal(TokenResponse{AccessToken: "access-token"})
		require.NoError(t, err)

		token, err := AccessTokenFromData(data)
		require.NoError(t, err)
		require.Equal(t, "access-token", token)
	})

	t.Run("兼容旧字符串响应", func(t *testing.T) {
		data, err := json.Marshal("legacy-token")
		require.NoError(t, err)

		token, err := AccessTokenFromData(data)
		require.NoError(t, err)
		require.Equal(t, "legacy-token", token)
	})

	t.Run("拒绝缺少访问令牌", func(t *testing.T) {
		token, err := AccessTokenFromData(json.RawMessage(`{"refresh_token":"refresh-token"}`))
		require.Error(t, err)
		require.Empty(t, token)
	})
}
