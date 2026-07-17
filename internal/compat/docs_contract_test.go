package compat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	return root
}

func TestCurrentDocsDescribeTrustedShopBoundaries(t *testing.T) {
	root := repositoryRoot(t)
	required := map[string][]string{
		"examples/06-shop-microservices/README.md": {
			"WithInternalCallers",
			"SupplierOrder",
			"requestID",
		},
		"docs/codex/ROUTERINFO_RUNTIME_GUIDE.md": {
			"可信内部调用方",
			"x-internal-callers",
		},
		"docs/codex/GRPC_TRANSPORT_MIGRATION.md": {
			"mTLS SAN",
			"SourceService",
		},
		".codex/skills/use-digitalway-core/SKILL.md": {
			"WithInternalCallers",
			"SupplierOrder",
		},
	}
	for name, fragments := range required {
		contents, err := os.ReadFile(filepath.Join(root, name))
		require.NoError(t, err)
		for _, fragment := range fragments {
			require.Contains(t, string(contents), fragment, name)
		}
	}

	readme, err := os.ReadFile(filepath.Join(root, "examples/06-shop-microservices/README.md"))
	require.NoError(t, err)
	require.NotContains(t, string(readme), "supplier-service/api/call")
}
