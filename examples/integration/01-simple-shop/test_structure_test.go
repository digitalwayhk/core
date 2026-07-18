package simpleshop_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPIsHaveIndependentTestMethods 约束每个 API 或 Manage command 都有独立的测试入口。
func TestAPIsHaveIndependentTestMethods(t *testing.T) {
	t.Helper()
	functions := make(map[string]*ast.FuncDecl)
	for _, name := range []string{"manage_test.go", "public_test.go", "private_test.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(".", name), nil, 0)
		require.NoError(t, err)
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil {
				functions[function.Name.Name] = function
			}
		}
	}

	groups := map[string][]string{
		"TestManageAPIs": {
			"testProductManageViewCommand",
			"testProductManageAddCommand",
			"testProductManageSearchCommand",
			"testProductManageEditCommand",
			"testProductManageRemoveCommand",
			"testOrderManageViewCommand",
			"testOrderManageSearchCommand",
			"testOrderManageSearchAndForeignSearch",
			"testOrderManageAddCommandNotRegistered",
			"testOrderManageEditCommandNotRegistered",
			"testOrderManageRemoveCommandNotRegistered",
		},
		"TestPublicAPIs": {
			"testGetProductsAPI",
		},
		"TestPrivateAPIs": {
			"testAddOrderAPI",
			"testGetOrdersAPI",
			"testDeleteOrderAPI",
			"testGetOrdersWebSocketAPI",
		},
	}
	for aggregate, expected := range groups {
		entry, exists := functions[aggregate]
		require.True(t, exists, "缺少可一次运行分组内所有 API 的总入口 %s", aggregate)
		called := calledFunctions(entry)
		for _, name := range expected {
			_, exists := functions[name]
			assert.True(t, exists, "缺少独立测试方法 %s", name)
			assert.Contains(t, called, name, "%s 未调用子测试 %s", aggregate, name)
		}
	}
}

func calledFunctions(function *ast.FuncDecl) []string {
	var names []string
	ast.Inspect(function.Body, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok {
			names = append(names, identifier.Name)
		}
		return true
	})
	return names
}
