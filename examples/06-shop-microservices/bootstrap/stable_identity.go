// 本文件为新建独立状态卷显式启用稳定身份，不自动迁移历史目录。
package bootstrap

import (
	"github.com/digitalwayhk/core/pkg/server/config"
	"os"
	"path/filepath"
	"strings"
)

func applyStableIdentity(c *config.ServerConfig) {
	root := strings.TrimSpace(os.Getenv("SHOP_IDENTITY_ROOT"))
	if root == "" {
		return
	}
	c.Cluster.IdentityMode = "stable"
	c.Cluster.IdentityFile = filepath.Join(root, c.Name, "identity.json")
	c.IDGenerator = &config.IDGeneratorConfig{
		Provider: "redis", Namespace: strings.TrimSpace(os.Getenv("SHOP_GENERATOR_NAMESPACE")), Epoch: strings.TrimSpace(os.Getenv("SHOP_GENERATOR_EPOCH")),
		Providers: config.ClusterProviderConfig{Redis: config.RedisProviderConfig{Addr: strings.TrimSpace(os.Getenv("SHOP_GENERATOR_REDIS_ADDR"))}},
	}
}
