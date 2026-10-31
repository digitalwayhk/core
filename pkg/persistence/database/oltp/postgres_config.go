// 本文件安全构造 PostgreSQL DSN 与各 SQL 存储的内部连接身份。
package oltp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"math"
	"net"
	"net/url"
	"strconv"
)

func (p *Postgres) dsn(database string) string {
	c := p.config
	u := url.URL{Scheme: "postgres", User: url.UserPassword(c.Username, c.Password), Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: "/" + database}
	q := u.Query()
	q.Set("sslmode", c.SSLMode)
	q.Set("TimeZone", c.TimeZone)
	q.Set("connect_timeout", strconv.Itoa(int(math.Ceil(c.ConnectTimeout.Seconds()))))
	if c.SSLRootCert != "" {
		q.Set("sslrootcert", c.SSLRootCert)
	}
	if c.SSLCert != "" {
		q.Set("sslcert", c.SSLCert)
		q.Set("sslkey", c.SSLKey)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func storageIdentity(c types.StorageConfig, name string) string {
	// 配置整体摘要包含凭证与池参数变化，但不会把它们暴露在键或日志里。
	c.Database = name
	return opaqueStorageIdentity(c.Driver, c)
}

func opaqueStorageIdentity(driver string, value interface{}) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return driver + ":" + hex.EncodeToString(digest[:])
}
