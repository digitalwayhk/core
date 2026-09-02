package compat

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExampleManageModelListBoundary(t *testing.T) {
	root := repositoryRoot(t)
	var violations []string
	err := filepath.WalkDir(filepath.Join(root, "examples"), func(path string, entry os.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		if !strings.Contains(filepath.ToSlash(rel), "/api/manage/") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "NewModelList" || selector.Sel.Name == "IDataAction" || strings.HasSuffix(selector.Sel.Name, "DataAction") {
				violations = append(violations, rel+":"+selector.Sel.Name)
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, violations, "api/manage 只能调用 models.NewManageModelList，不得感知 ModelList 构造或 IDataAction: %v", violations)

	modelFacades := []string{
		"examples/01-simple-shop/models/data_action.go",
		"examples/02-shop-payment/models/data_action.go",
		"examples/03-shop-inheritance/models/data_action.go",
		"examples/04-shop-performance/models/data_action.go",
		"examples/05-shop-casdoor-rbac/models/models.go",
		"examples/06-shop-microservices/user-service/models/models.go",
		"examples/06-shop-microservices/supplier-service/models/models.go",
		"examples/06-shop-microservices/order-service/models/models.go",
		"examples/07-shop-order-scale/supplier-service/models/models.go",
		"examples/07-shop-order-scale/order-service/models/models.go",
	}
	for _, name := range modelFacades {
		requireFileContains(t, filepath.Join(root, name), "func NewManageModelList[")
	}
}
