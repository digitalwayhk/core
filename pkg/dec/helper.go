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

func PublishEvent[T struct{}](domain string, eventCode string, eventData T) {
	eventContent := map[string]interface{}{}
	eventContent[EventDataKey] = util.JsonUtil.ToString(eventData)
	ctx := Client.CreateEventContext(context.Background(), domain, "", eventCode, eventContent)
	Client.PublishEvent(ctx)
}

func GetEventData[T struct{}](request subscribe.Request) T {
	value := request.ParamsParser.GetString(EventDataKey)
	var eventData T
	_ = json.Unmarshal([]byte(value), &eventData)
	return eventData
}
