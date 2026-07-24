package compat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestRouterRuntimeHasNoProcessGlobalMutableComponents(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	files := []string{
		"pkg/server/types/routerinfo.go",
		"pkg/server/types/websocketshard.go",
		"pkg/server/types/websocketnotificationsystem.go",
	}
	for _, name := range files {
		data, readErr := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, readErr)
		source := string(data)
		for _, forbidden := range []string{
			"clearMap", "websocketcleanupOnce", "noticeJobChan", "jobChan", "sync.Pool",
			"rArgs", "rHashClients", "rCache",
		} {
			require.False(t, strings.Contains(source, forbidden), "%s 仍包含进程级可变组件 %q", name, forbidden)
		}
	}
}

func TestRouterInfoCompatibilityMethodsRemain(t *testing.T) {
	var info *types.RouterInfo
	_ = info.RegisterWebSocketClient
	_ = info.UnRegisterWebSocketClient
	_ = info.NoticeWebSocket
	_ = info.CleanupDeadConnections
	_ = info.GetActiveClientCount
	_ = info.UseCache
	_ = types.StartPeriodicCleanup
	_ = func(ctx context.Context) error { return types.StopPeriodicCleanup(ctx) }
	_ = types.SetCrossNodeForwarder
	_ = types.GetCrossNodeForwarder
}
