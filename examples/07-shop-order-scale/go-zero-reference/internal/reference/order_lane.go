// 本文件给同一订单选择固定通道，不依赖 Snowflake 的序列低位。
package reference

import (
	"hash/fnv"
	"strconv"
)

func orderLane(orderID, workers uint) uint {
	h := fnv.New64a()
	_, _ = h.Write([]byte("shop-order:order:" + strconv.FormatUint(uint64(orderID), 10)))
	return uint(h.Sum64() % uint64(workers))
}
