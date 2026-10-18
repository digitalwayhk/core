// 本文件按认证与驱动语义隔离共享池；运行时键不包含可离线猜解的固定密码摘要。
package oltp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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
	if canonical.Charset == "" {
		canonical.Charset = "utf8mb4"
	}
	if canonical.Loc == "" {
		canonical.Loc = "Local"
	}
	// 仅编码驱动实际使用字段；parseTime始终true，池容量/日志沿用首次拥有者。
	// 长度前缀无损保留原始字节，避免分隔符歧义或JSON非法UTF8替换。
	data := make([]byte, 0, 128)
	for _, field := range []string{canonical.Host, canonical.Username, canonical.Password, name, canonical.Charset, canonical.Loc} {
		data = binary.AppendUvarint(data, uint64(len(field)))
		data = append(data, field...)
	}
	data = binary.AppendVarint(data, int64(canonical.Port))
	if canonical.InterpolateParams {
		data = append(data, 1)
	} else {
		data = append(data, 0)
	}
	mac := hmac.New(sha256.New, mysqlPoolIdentitySecret)
	_, _ = mac.Write(data)
	return fmt.Sprintf("%s:%d/%s#%x", canonical.Host, canonical.Port, name, mac.Sum(nil))
}
