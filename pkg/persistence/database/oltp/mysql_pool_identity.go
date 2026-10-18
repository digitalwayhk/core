// 本文件按认证与驱动语义隔离共享池；运行时键不包含可离线猜解的固定密码摘要。
package oltp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
)

var mysqlPoolIdentitySecret = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("初始化MySQL连接池身份失败")
	}
	return key
}()

func mysqlConnectionIdentity(config *Config, name string) string {
	canonical := *config
	canonical.Database = name
	if canonical.Charset == "" {
		canonical.Charset = "utf8mb4"
	}
	canonical.Charset = strings.ToLower(canonical.Charset)
	if canonical.Loc == "" {
		canonical.Loc = "Local"
	}
	canonical.ParseTime = true // 当前驱动DSN始终启用parseTime。
	// 池容量和日志沿用首次拥有者配置，不另建同认证池。
	canonical.MaxIdleConns = 0
	canonical.MaxOpenConns = 0
	canonical.MaxLifetime = 0
	canonical.IsLog = false
	data, err := json.Marshal(canonical)
	if err != nil {
		panic("无法编码MySQL连接池身份")
	}
	mac := hmac.New(sha256.New, mysqlPoolIdentitySecret)
	_, _ = mac.Write(data)
	return fmt.Sprintf("%s:%d/%s#%x", canonical.Host, canonical.Port, name, mac.Sum(nil))
}
