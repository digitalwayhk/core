// 本文件用独立 Snowflake worker 生成下单及事件记录 ID，拒绝时钟回退。
package reference

import (
	"errors"
	"sync"
	"time"
)

type clock struct {
	mu       sync.Mutex
	last     int64
	sequence uint
	worker   uint
}

func (c *clock) next() (uint, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		now := time.Now().UnixMilli() - 1577836800000
		if now < c.last {
			return 0, errors.New("Snowflake 时钟回退")
		}
		if now == c.last {
			c.sequence++
			if c.sequence > 4095 {
				time.Sleep(time.Millisecond)
				continue
			}
		} else {
			c.sequence = 0
		}
		c.last = now
		return uint(now)<<22 | c.worker<<12 | c.sequence, nil
	}
}
