// 本文件验证就绪检查使用服务自身状态，不把存活伪装成注册可用。
package public

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReadyEndpointContract 验证独立路由契约与无服务进程的就绪行为。
func TestReadyEndpointContract(t *testing.T) {
	r := &Ready{}
	require.Equal(t, "/api/ready", r.RouterInfo().GetPath())
	res, err := r.Do(nil)
	require.NoError(t, err)
	require.Equal(t, "ready", res.(*HealthResponse).Status)
}
