package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/stretchr/testify/require"
)

// TestExampleConfigsAreValid 验证仓库随示例提供的两份配置可以通过当前配置契约。
func TestExampleConfigsAreValid(t *testing.T) {
	repoRoot, err := repositoryRoot()
	require.NoError(t, err)
	for _, name := range []string{"server", "shop"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(repoRoot, "examples", "01-simple-shop", "main", "etc", name+".json")
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			var serviceConfig config.ServerConfig
			require.NoError(t, json.Unmarshal(data, &serviceConfig))
			serviceConfig.ApplyDefaults()
			require.NoError(t, serviceConfig.Validate())
		})
	}
}
