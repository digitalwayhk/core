// 本文件按独立编号配置构建资源，不继承发现模式或自动降级。
package identity

import (
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/config"
)

// BuildAuthority 构建尚未初始化的独立编号权威和可审计连接指纹。
func BuildAuthority(c *config.IDGeneratorConfig) (*Authority, string, error) {
	if c == nil {
		return nil, "", fmt.Errorf("identity: generator configuration required")
	}
	if err := c.Validate(); err != nil {
		return nil, "", err
	}
	key := "core:generator:" + c.Namespace
	var store Store
	var name string
	switch c.Provider {
	case "local":
		store = NewFileStore(c.LocalPath)
		name = "local:" + c.LocalPath
	case "redis":
		r := c.Providers.Redis
		prefix := r.Prefix
		if prefix != "" {
			key = prefix + ":" + key
		}
		store = NewRedisStore(r.Addr, r.DB, key)
		name = fmt.Sprintf("redis:%s:%d:%s", r.Addr, r.DB, key)
	case "etcd":
		r := c.Providers.Etcd
		prefix := r.Prefix
		if prefix == "" {
			prefix = "/core/generator"
		}
		key = prefix + "/" + c.Namespace
		var err error
		store, err = NewEtcdStore(r.Endpoints, key)
		if err != nil {
			return nil, "", err
		}
		name = fmt.Sprintf("etcd:%q:%s", r.Endpoints, key)
	default:
		return nil, "", fmt.Errorf("identity: unsupported generator provider %q", c.Provider)
	}
	return NewAuthority(store, c.Epoch), name, nil
}
