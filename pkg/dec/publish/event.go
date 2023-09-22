package publish

import "github.com/digitalwayhk/core/pkg/dec/subscribe"

type Event struct {
	domain       string
	entityId     string
	eventCode    string
	eventContent map[string]interface{}

	eventId         string
	publishTime     uint32
	subscribeList   []string
	subscribeResult []subscribe.Result
	retryTimes      uint32
}

func (e *Event) GetDomain() string {
	return e.domain
}

func (e *Event) GetEntityId() string {
	return e.entityId
}

func (e *Event) GetEventCode() string {
	return e.eventCode
}

func (e *Event) GetEventContent() map[string]interface{} {
	return e.eventContent
}

func (e *Event) GetEventId() string {
	return e.eventId
}

func (e *Event) GetPublishTime() uint32 {
	return e.publishTime
}

func (e *Event) SetEntityId(entityId string) {
	e.entityId = entityId
}

func (e *Event) SetParam(key string, value interface{}) {
	e.eventContent[key] = value
}
