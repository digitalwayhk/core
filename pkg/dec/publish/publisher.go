package publish

import (
	"context"
	"github.com/digitalwayhk/core/pkg/dec/eventbus"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"
	"github.com/gofrs/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"strings"
	"time"
)

var Publisher = newPublisher()

type publisher struct {
}

func newPublisher() *publisher {
	return &publisher{}
}

func (p *publisher) NewEvent(domain string, entityId string, eventCode string, eventContent map[string]interface{}) *Event {
	m := make(map[string]interface{})
	if len(eventContent) > 0 {
		for k, v := range eventContent {
			m[k] = v
		}
	}
	subscribeList := eventbus.EventBus.GetSubscriberList(domain, eventCode)
	uuidStr, _ := uuid.NewV4()
	return &Event{
		domain:       domain,
		entityId:     entityId,
		eventCode:    eventCode,
		eventContent: m,

		eventId:         strings.ReplaceAll(uuidStr.String(), "-", ""),
		publishTime:     uint32(time.Now().Unix()),
		subscribeList:   subscribeList,
		subscribeResult: make([]subscribe.Result, len(subscribeList)),
		retryTimes:      0,
	}
}

func (p *publisher) PublishEvent(ctx context.Context, event Event) []subscribe.Result {
	size := len(event.subscribeList)
	if size == 0 {
		logx.Infof("PublishEvent subscribeList is null,event:%v", event)
		return event.subscribeResult
	}
	request := buildSubscribeRequest(event)
	eventbus.EventBus.Notify(ctx, request)
	logx.Infof("PublishEvent finish,event:%v", event)
	return event.subscribeResult
}

func buildSubscribeRequest(event Event) subscribe.RequestWrapper {
	req := subscribe.NewRequestWrapper(event.eventId, event.publishTime, event.domain, event.entityId, event.eventCode, event.eventContent, event.subscribeList, event.subscribeResult, event.retryTimes)
	return *req
}
