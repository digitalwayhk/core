// 本文件锁定现行指南、示例和 skill 中必须同步维护的跨服务与 OpenAPI 安全契约。
package compat

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
			"/api/openapi",
			"/api/internal/openapi",
			"ServerManageAuth",
		},
		"docs/codex/FRAMEWORK_USAGE_GUIDE.md": {
			"/api/openapi",
			"/api/internal/openapi",
			"ServerManageAuth",
		},
		"docs/codex/GRPC_TRANSPORT_MIGRATION.md": {
			"mTLS SAN",
			"SourceService",
		},
		".codex/skills/use-digitalway-core/SKILL.md": {
			"WithInternalCallers",
			"SupplierOrder",
			"/api/openapi",
			"/api/internal/openapi",
			"ServerManageAuth",
		},
		".codex/skills/use-digitalway-core/references/core-backend-api.md": {
			"/api/openapi",
			"/api/internal/openapi",
			"x-internal-callers",
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

func TestExample06StructureFollowsServiceModelConventions(t *testing.T) {
	root := repositoryRoot(t)
	skill, err := os.ReadFile(filepath.Join(root, ".codex/skills/use-digitalway-core/references/core-backend-api.md"))
	require.NoError(t, err)
	require.NotContains(t, string(skill), "api/call 目标 API")

	for _, service := range []string{"user-service", "supplier-service", "order-service"} {
		modelRoot := filepath.Join(root, "examples/06-shop-microservices", service, "models")
		for _, subdir := range []string{"common", "basedata", "transaction", "internal/store", "schema"} {
			info, err := os.Stat(filepath.Join(modelRoot, subdir))
			require.NoError(t, err, "%s 必须按 05 示例拆出 models/%s", service, subdir)
			require.True(t, info.IsDir(), "%s models/%s 必须是目录", service, subdir)
		}
		requireOnlyFacadeGoFiles(t, modelRoot, "models.go")

		manageRoot := filepath.Join(root, "examples/06-shop-microservices", service, "api/manage")
		for _, subdir := range []string{"common", "basedata", "transaction"} {
			info, err := os.Stat(filepath.Join(manageRoot, subdir))
			require.NoError(t, err, "%s 必须按 05 示例拆出 api/manage/%s", service, subdir)
			require.True(t, info.IsDir(), "%s api/manage/%s 必须是目录", service, subdir)
		}
		requireOnlyFacadeGoFiles(t, manageRoot, "manage.go")
		requireFileContains(t, filepath.Join(manageRoot, "common", "service_manage.go"), "type ServiceManage")
		requireFileContains(t, filepath.Join(manageRoot, "common", "service_manage.go"), "NewHookedManageService")
		requireFileContains(t, filepath.Join(manageRoot, "common", "service_manage.go"), "shop_manage_operation_failed")
		requireFileContains(t, filepath.Join(manageRoot, "common", "service_manage.go"), "shop_manage_operation_succeeded")
		requireFileNotContains(t, filepath.Join(manageRoot, "common", "service_manage.go"), "shop_user_manage_operation")
		requireFileNotContains(t, filepath.Join(manageRoot, "common", "service_manage.go"), "shop_supplier_manage_operation")
		requireFileNotContains(t, filepath.Join(manageRoot, "common", "service_manage.go"), "shop_order_manage_operation")
		requireFileContains(t, filepath.Join(manageRoot, "basedata", "base_data_manage.go"), "type BaseDataManage")
		requireFileContains(t, filepath.Join(manageRoot, "transaction", "transaction_manage.go"), "type TransactionManage")
		requireFileContains(t, filepath.Join(manageRoot, "common", "service_manage.go"), "logx.Infow")
		requireConcreteManageUsesLocalBase(t, filepath.Join(manageRoot, "basedata"), "BaseDataManage")
		requireConcreteManageUsesLocalBase(t, filepath.Join(manageRoot, "transaction"), "TransactionManage")
		requireConcreteManageDoesNotRepeatServiceAuthorization(t, manageRoot)
	}

	requireFileNotContains(t, filepath.Join(root, "examples/06-shop-microservices/main/supplier/main.go"), "IsWebSocket: true")
	requireFileNotContains(t, filepath.Join(root, "examples/06-shop-microservices/main/all-in-one/main.go"), "supplierservice.Service{}, &servertypes.ServerOption{IsWebSocket: true}")
	requireFileNotContains(t, filepath.Join(root, "examples/06-shop-microservices/order-service/business/order.go"), "func SupplierOrders(")
}

func TestExample06ProductionFilesAreSplitByStruct(t *testing.T) {
	root := repositoryRoot(t)
	structDecl := regexp.MustCompile(`(?m)^type\s+\w+\s+struct\b`)
	err := filepath.WalkDir(filepath.Join(root, "examples/06-shop-microservices"), func(path string, entry os.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		if strings.Contains(rel, "/main/") {
			return nil
		}
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		matches := structDecl.FindAll(contents, -1)
		require.LessOrEqual(t, len(matches), 1, "%s 包含多个 struct，应按 struct 拆文件", rel)
		return nil
	})
	require.NoError(t, err)
}

func requireFileContains(t *testing.T, path, fragment string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(contents), fragment, path)
}

func requireFileNotContains(t *testing.T, path, fragment string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(contents), fragment, path)
}

func requireOnlyFacadeGoFiles(t *testing.T, dir, facade string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		require.Equal(t, facade, entry.Name(), "%s 根目录只允许保留 %s，业务实现必须进入语义子目录", dir, facade)
	}
}

func requireConcreteManageUsesLocalBase(t *testing.T, dir, baseName string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_manage.go") {
			continue
		}
		if entry.Name() == "base_data_manage.go" || entry.Name() == "transaction_manage.go" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		text := string(contents)
		require.NotContains(t, text, "*managepkg.ManageService", "%s 不能直接嵌入 ManageService，必须先继承本服务 %s", path, baseName)
		require.Contains(t, text, "*"+baseName, "%s 必须继承本目录的 %s", path, baseName)
	}
}

func requireConcreteManageDoesNotRepeatServiceAuthorization(t *testing.T, manageRoot string) {
	t.Helper()
	for _, dir := range []string{"basedata", "transaction"} {
		entries, err := os.ReadDir(filepath.Join(manageRoot, dir))
		require.NoError(t, err)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_manage.go") {
				continue
			}
			if entry.Name() == "base_data_manage.go" || entry.Name() == "transaction_manage.go" {
				continue
			}
			path := filepath.Join(manageRoot, dir, entry.Name())
			contents, err := os.ReadFile(path)
			require.NoError(t, err)
			text := string(contents)
			for _, fragment := range []string{
				"commonmanage.AdminOnly(",
				"commonmanage.AdminSearch(",
				"commonmanage.ActorFrom(",
				"commonmanage.ActorFromRequest(",
				"commonmanage.OwnerSearch(",
				"commonmanage.AddOwnerWhere(",
				"commonmanage.AuthorizeWrite(",
				"commonmanage.AuthorizeSupplierWrite(",
			} {
				require.NotContains(t, text, fragment, "%s 的服务级权限判断必须上移到 common.ServiceManage", path)
			}
		}
	}
}
