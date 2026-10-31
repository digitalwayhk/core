// 本文件从测试夹具读取独立进程的原生依赖配置，不加载 Core 运行时。
package reference

import (
	"errors"
	"os"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/rest"
)

// Config 声明原生三进程对照所需的配置；表由计时外的原夹具预建。
type Config struct {
	Role                  string
	HTTPPort, GRPCPort    int
	CoreJSON              string
	SupplierDB            string
	SupplierRPC, OrderRPC string
	PendingDir            string
}

type fileConfig struct {
	rest.RestConf
	Auth struct{ AccessSecret string }
}

func loadFile(path string) (fileConfig, error) {
	var c fileConfig
	b, e := os.ReadFile(path)
	if e != nil {
		return c, e
	}
	e = conf.LoadFromJsonBytes(b, &c)
	if e == nil && c.Name == "" {
		e = errors.New("缺少已准备的服务配置")
	}
	return c, e
}
