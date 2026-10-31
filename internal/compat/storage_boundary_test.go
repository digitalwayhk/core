// 本文件锁定数据库差异只在持久化边界处理，不回流到Manage或统计执行入口。
package compat

import (
	"github.com/stretchr/testify/require"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStorageBoundary 验证Manage不导入SQL驱动，统计入口不探测或编译数据库方言。
func TestStorageBoundary(t *testing.T) {
	root := repositoryRoot(t)
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		require.NoError(t, err)
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || !strings.Contains(filepath.ToSlash(path), "/api/manage/") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		require.NoError(t, err)
		for _, imp := range file.Imports {
			require.NotContains(t, imp.Path.Value, "gorm.io/driver/", path)
			require.NotContains(t, imp.Path.Value, "pgx", path)
		}
		return nil
	}))
	path := filepath.Join(root, "pkg/persistence/entity/stats/exec.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(t, err)
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "Exec" {
			continue
		}
		ast.Inspect(function, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok {
				require.NotContains(t, []string{"Dialector", "Raw", "compileOLTP"}, selector.Sel.Name)
			}
			return true
		})
	}
}
