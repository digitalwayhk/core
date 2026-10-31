// 本文件集中创建 OLTP 存储；models 与控制面只声明配置。
package oltp

import (
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

// NewDataAction 根据已验证配置创建存储，构造期间不建立连接。
func NewDataAction(c types.StorageConfig) (types.IDataAction, error) {
	c.ApplyDefaults()
	if err := c.Validate(false); err != nil {
		return nil, err
	}
	switch c.Driver {
	case "sqlite":
		return NewFixedSqlite(c.Database), nil
	case "mysql":
		return NewMySQL(&Config{IsLog: c.IsLog, Host: c.Host, Port: c.Port, Username: c.Username, Password: c.Password, Database: c.Database, Charset: "utf8mb4", ParseTime: true, Loc: "Local", MaxIdleConns: c.MaxIdleConns, MaxOpenConns: c.MaxOpenConns, MaxLifetime: c.MaxLifetime}), nil
	case "postgres":
		return NewPostgres(c), nil
	default:
		return nil, fmt.Errorf("Driver %q is not implemented", c.Driver)
	}
}
