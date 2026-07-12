package config

import (
	"os"
	"reflect"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const capabilityMatrixPath = "../../../docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md"

func TestCapabilityMatrixListsEveryProjectServerConfigField(t *testing.T) {
	matrix, err := os.ReadFile(capabilityMatrixPath)
	require.NoError(t, err)

	paths := projectConfigFieldPaths(reflect.TypeOf(ServerConfig{}), "ServerConfig")
	for _, path := range paths {
		assert.Contains(t, string(matrix), "`"+path+"`", "能力矩阵缺少机器检查字段")
	}
}

func TestCapabilityMatrixHasNoAcceptedButIgnoredStatus(t *testing.T) {
	matrix, err := os.ReadFile(capabilityMatrixPath)
	require.NoError(t, err)

	assert.NotContains(t, string(matrix), "accepted-but-ignored")
}

func projectConfigFieldPaths(typ reflect.Type, prefix string) []string {
	var paths []string
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.IsExported() {
			continue
		}
		path := prefix + "." + field.Name
		paths = append(paths, path)

		// go-zero RestConf 由上游维护，只跟踪嵌入点；项目自有配置结构继续递归。
		if field.Type.Kind() == reflect.Struct && field.Type.PkgPath() == typ.PkgPath() {
			paths = append(paths, projectConfigFieldPaths(field.Type, path)...)
		}
	}
	sort.Strings(paths)
	return paths
}
