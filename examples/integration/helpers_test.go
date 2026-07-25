package integration

import (
	"encoding/json"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReservePortRangeUsesDedicatedLowRange(t *testing.T) {
	const (
		count    = 4
		rangeMin = 20000
		rangeMax = 44999
	)
	base, err := reservePortRange(count)
	require.NoError(t, err)
	require.GreaterOrEqual(t, base, rangeMin)
	require.LessOrEqual(t, base+count-1, rangeMax)

	listeners := make([]net.Listener, 0, count)
	t.Cleanup(func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	})
	for offset := 0; offset < count; offset++ {
		listener, listenErr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+offset)))
		require.NoError(t, listenErr)
		listeners = append(listeners, listener)
	}
}

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
