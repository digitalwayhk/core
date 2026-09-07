// 本文件定义本地缓存的通知对账版本，不改变 Redis 权威缓存格式。
package routecache

import "encoding/json"

type notificationCacheRecord struct {
	Epoch uint64          `json:"notification_epoch"`
	Data  json.RawMessage `json:"data"`
}
