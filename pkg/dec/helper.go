package dec

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"
	"github.com/digitalwayhk/core/pkg/dec/util"
)

const (
	EventDataKey = "event_data"
)

type IEventData interface {
}

// 异步发布并行事件（开协程并发消费）
func AsyncPublishParallelEvent[T IEventData](domain string, eventCode string, eventData T) {
	eventContent := map[string]interface{}{}
	eventContent[EventDataKey] = util.JsonUtil.ToString(eventData)
	ctx := Client.CreateEventContext(context.Background(), domain, "", eventCode, eventContent)
	Client.PublishEvent(ctx)
}

// 异步发布有序事件（通过chan有序消费）
func AsyncPublishOrderedEvent[T IEventData](domain string, eventCode string, eventData T) {
	eventContent := map[string]interface{}{}
	eventContent[EventDataKey] = util.JsonUtil.ToString(eventData)
	ctx := Client.CreateEventContext(context.Background(), domain, "", eventCode, eventContent)
	Client.PublishOrderedEvent(ctx)
}

// 同步发布事件
func SyncPublishEvent[T IEventData](domain string, eventCode string, eventData T) error {
	eventContent := map[string]interface{}{}
	eventContent[EventDataKey] = util.JsonUtil.ToString(eventData)
	ctx := Client.CreateEventContext(context.Background(), domain, "", eventCode, eventContent)
	return Client.SyncPublishEvent(ctx)
}

// 获取事件数据
func GetEventData[T IEventData](request subscribe.Request) T {
	value := request.ParamsParser.GetString(EventDataKey)
	var eventData T
	_ = json.Unmarshal([]byte(value), &eventData)
	return eventData
}
