// 本文件验证统一存储装配的默认值、固定库边界及未知驱动拒绝。
package oltp

import (
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"testing"
)

// TestStorageFactoryDefaults 验证 models 只需声明存储配置。
func TestStorageFactoryDefaults(t *testing.T) {
	c := types.StorageConfig{Driver: "postgres", Host: "localhost", Username: "core", Database: "test"}
	c.ApplyDefaults()
	require.Equal(t, 5432, c.Port)
	require.NoError(t, c.Validate(true))
	c.Database = ""
	require.NoError(t, c.Validate(false))
	require.Error(t, c.Validate(true))
	c.Driver = "unknown"
	require.Error(t, c.Validate(false))
	_, err := NewDataAction(c)
	require.Error(t, err)
	action, err := NewDataAction(types.StorageConfig{Driver: "sqlite", Database: "factory_test"})
	require.NoError(t, err)
	require.IsType(t, &Sqlite{}, action)
}
