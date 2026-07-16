package runtime

import (
	"context"
	"fmt"
)

type ExternalControlSubscriber interface {
	SubscribeExternalControl(ctx context.Context, subject string) (func(), error)
}

// SubscribeExternalControls 以全有或全无方式建立控制事件订阅。
// 任一主题失败时立即撤销本轮已建立的订阅，避免服务在通知链路残缺时继续运行。
func SubscribeExternalControls(ctx context.Context, subscriber ExternalControlSubscriber, subjects ...string) ([]func(), error) {
	cancels := make([]func(), 0, len(subjects))
	for _, subject := range subjects {
		cancel, err := subscriber.SubscribeExternalControl(ctx, subject)
		if err != nil {
			for index := len(cancels) - 1; index >= 0; index-- {
				cancels[index]()
			}
			return nil, fmt.Errorf("subscribe external control subject %q: %w", subject, err)
		}
		cancels = append(cancels, cancel)
	}
	return cancels, nil
}
