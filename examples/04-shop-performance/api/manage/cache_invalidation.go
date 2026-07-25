package manage

import (
	"context"

	"github.com/digitalwayhk/core/examples/04-shop-performance/business"
	"github.com/zeromicro/go-zero/core/logx"
)

// invalidateOrderReferenceBestEffort 在业务持久化已成功后发布失效事件。
// 控制事件失败必须记录可观察异常，但不能让客户端误以为已提交的数据写入失败。
func invalidateOrderReferenceBestEffort(operation string) {
	if err := business.InvalidateOrderReferenceCache(context.Background()); err != nil {
		logx.Errorw("order_reference_cache_invalidation_failed",
			logx.Field("operation", operation),
			logx.Field("error", err),
		)
	}
}
