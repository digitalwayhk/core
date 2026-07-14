package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommonHelpersContainNoShopBusiness 验证公共集成测试层不持有商城 DTO、路由或业务方法。
func TestCommonHelpersContainNoShopBusiness(t *testing.T) {
	source, err := os.ReadFile("helpers.go")
	require.NoError(t, err)
	content := string(source)
	for _, forbidden := range []string{
		"ProductDTO", "OrderDTO", "OrderEvent", "StartShopSuite",
		"AddProduct", "ReadOrderEvent", "ProductNames", "OrderIDs",
		"/api/shop/", "writeServiceConfig", "ValidateServiceConfigs",
	} {
		assert.NotContains(t, content, forbidden)
	}
}

// TestSimpleShopCommitsNoRuntimeConfig 验证示例配置由首次运行生成，不作为源码提交。
func TestSimpleShopCommitsNoRuntimeConfig(t *testing.T) {
	root, err := repositoryRoot()
	require.NoError(t, err)
	for _, name := range []string{"server.json", "shop.json"} {
		path := filepath.Join(root, "examples", "01-simple-shop", "main", "etc", name)
		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err), strings.TrimPrefix(path, root+string(filepath.Separator)))
	}
}
