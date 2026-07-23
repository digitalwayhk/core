// 本文件保留旧的毫秒时间戳文本转换入口。
package utils

import (
	"strconv"
	"time"
)

// ToTime 将毫秒时间戳文本转换为 time.Time，解析失败时沿用旧的零时间戳行为。
func ToTime(s string) time.Time {
	timestamp, _ := strconv.ParseInt(s, 10, 64)
	return time.Unix(timestamp/1000, 0)
}
