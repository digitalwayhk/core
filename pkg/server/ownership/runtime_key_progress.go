// 本文件只记录有限归属键的本地确认时钟，不保存消息副本或业务会话账。
package ownership

import (
	"context"
	"sync/atomic"
)

type runtimeKeyProgress struct {
	context    context.Context
	credential Credential
	confirmed  atomic.Int64
	stageSince *atomic.Int64
}
