// 本文件验证 HTMLServer 内嵌前端产物的入口与异步资源闭包，防止混入多套构建导致浏览器白屏。
package run

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var (
	htmlAssetReferencePattern = regexp.MustCompile(`(?:src|href)="(/[^"]+)"`)
	jsChunkReferencePattern   = regexp.MustCompile(`[0-9A-Za-z_-]+-async\.[0-9a-f]+\.(?:js|css)`)
)

// TestEmbeddedAdminDistReferencesExistingAssets 验证入口文件及主 Bundle 引用的资源全部存在。
func TestEmbeddedAdminDistReferencesExistingAssets(t *testing.T) {
	index, err := fs.ReadFile(html, "dist/index.html")
	require.NoError(t, err)

	checked := make(map[string]struct{})
	for _, match := range htmlAssetReferencePattern.FindAllStringSubmatch(string(index), -1) {
		asset := strings.TrimPrefix(strings.SplitN(match[1], "?", 2)[0], "/")
		requireEmbeddedAdminAsset(t, asset)
		checked[asset] = struct{}{}

		if !strings.HasSuffix(asset, ".js") {
			continue
		}
		bundle, readErr := fs.ReadFile(html, "dist/"+asset)
		require.NoError(t, readErr)
		for _, chunk := range jsChunkReferencePattern.FindAllString(string(bundle), -1) {
			if _, ok := checked[chunk]; ok {
				continue
			}
			requireEmbeddedAdminAsset(t, chunk)
			checked[chunk] = struct{}{}
		}
	}
}

func requireEmbeddedAdminAsset(t *testing.T, asset string) {
	t.Helper()
	_, err := fs.Stat(html, "dist/"+asset)
	require.NoErrorf(t, err, "内嵌前端引用了不存在的资源 %s", asset)
}
