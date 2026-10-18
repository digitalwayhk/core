// 本文件验证 07 all-in-one 默认端口及显式启动参数的优先级。
package main

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/stretchr/testify/require"
)

// TestDefaultPortsRespectArguments 验证默认值不覆盖 -p/-grpc 及其标准 flag 写法。
func TestDefaultPortsRespectArguments(t *testing.T) {
	for _, tc := range []struct {
		args []string
		http int
		grpc int
	}{
		{nil, 48180, 58180},
		{[]string{"-p", "23000"}, 0, 58180},
		{[]string{"--p=23000", "-grpc=33000"}, 0, 0},
		{[]string{"-p=23000", "--grpc", "33000"}, 0, 0},
	} {
		server := &run.WebServer{}
		setDefaultPorts(server, tc.args)
		require.Equal(t, tc.http, server.Port)
		require.Equal(t, tc.grpc, server.GRPCPort)
	}
}
