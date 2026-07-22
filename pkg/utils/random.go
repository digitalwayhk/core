// 本文件保留旧的进程级伪随机数兼容入口。
package utils

import (
	"math/rand"
	"time"
)

// GetRandNum 返回区间 [0,n) 内的伪随机整数。
func GetRandNum(n int) int {
	rand.Seed(time.Now().Unix())
	return rand.Intn(n)
}
