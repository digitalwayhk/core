// 本文件验证 07 示例开发 Swagger 跨端口访问各业务 REST 服务的 CORS 配置。
package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSwaggerServerOptionAllowsLocalDevelopmentOrigins(t *testing.T) {
	option := SwaggerServerOption(true)

	require.True(t, option.IsWebSocket)
	require.True(t, option.IsCors)
	require.Equal(t, []string{"http://localhost", "http://127.0.0.1"}, option.OriginCors)
}

func TestSwaggerServerOptionReturnsIndependentOrigins(t *testing.T) {
	first := SwaggerServerOption(false)
	first.OriginCors[0] = "https://mutated.example.com"

	second := SwaggerServerOption(false)

	require.False(t, second.IsWebSocket)
	require.Equal(t, "http://localhost", second.OriginCors[0])
}

func TestLocalServiceConfigEnablesLocalRuntimePrometheusByDefault(t *testing.T) {
	t.Setenv("SHOP_RUNTIME_PROM_URL", "")

	cfg := LocalServiceConfig("shop-user", 48181, 2, 1)

	require.Equal(t, "prometheus", cfg.RuntimeObservability.Mode)
	require.Equal(t, "http://127.0.0.1:19090", cfg.RuntimeObservability.QueryURL)
}
