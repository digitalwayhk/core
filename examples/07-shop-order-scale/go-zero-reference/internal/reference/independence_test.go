// 本文件检查独立应用及其协议包只依赖底层库，不把 Core 封装带进对照。
package reference

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReferenceDoesNotImportCore 防止原生实现以后误接入 Core Router、业务或持久化封装。
func TestReferenceDoesNotImportCore(t *testing.T) {
	root := filepath.Join("..", "..")
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, e := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if e != nil {
			return e
		}
		for _, i := range f.Imports {
			name, e := strconv.Unquote(i.Path.Value)
			require.NoError(t, e)
			if strings.HasPrefix(name, "github.com/digitalwayhk/core/") {
				require.True(t, strings.HasPrefix(name, "github.com/digitalwayhk/core/examples/07-shop-order-scale/go-zero-reference/"), "%s 导入了 Core 封装 %s", path, name)
			}
		}
		return nil
	}))
}

// TestReferenceUsesBusinessRPC 防止原生应用照搬通用 Payload/Call 封装而掩盖传输设计成本。
func TestReferenceUsesBusinessRPC(t *testing.T) {
	data, e := os.ReadFile(filepath.Join("..", "wire", "payload.proto"))
	require.NoError(t, e)
	for _, method := range []string{"GetProducts", "CreateOrder", "GetOrders"} {
		require.Contains(t, string(data), "rpc "+method+"(")
	}
	require.NotContains(t, string(data), "bytes data")
	require.NotContains(t, string(data), "rpc Call(")
}
