package subscribe

import (
	"context"
	"github.com/digitalwayhk/core/pkg/dec/util"
)

type Config struct {
	name         string
	subscribeMap map[string]map[string]Subscribe
}

type Subscribe struct {
	Domain string `json:"domain"`
	Event  string `json:"event"`
	//依赖
	Dependence []string `json:"dependence"`
	//最大重试次数，默认0，使用默认重试次数，-1表示不重试
	MaxRetryTimes int `json:"max_retry_times"` //TODO  add retry
}

func NewConfig(name string, subscribe []Subscribe) *Config {
	subscribeMap := buildSubscribeMap(subscribe)
	return &Config{name: name, subscribeMap: subscribeMap}
}

func NewDomainConfig(name string, domain string, event ...string) *Config {
	subscribe := NewDomainSubscribe(domain, event, nil, 0)
	return NewConfig(name, subscribe)
}

func buildSubscribeMap(subscribe []Subscribe) map[string]map[string]Subscribe {
	subscribeMap := make(map[string]map[string]Subscribe)
	for _, s := range subscribe {
		domain := s.Domain
		event := s.Event
		if eventMap, ok := subscribeMap[domain]; ok {
			eventMap[event] = s
			continue
		}
		eventMap := make(map[string]Subscribe)
		eventMap[event] = s
		subscribeMap[domain] = eventMap
	}
	return subscribeMap
}

func NewSubscribe(domain string, event string, dependence []string, maxRetryTimes int) *Subscribe {
	return &Subscribe{
		Domain:        domain,
		Event:         event,
		Dependence:    dependence,
		MaxRetryTimes: maxRetryTimes,
	}
}

func NewDomainSubscribe(domain string, event []string, dependence []string, maxRetryTimes int) []Subscribe {
	subscribe := make([]Subscribe, len(event))
	for index, e := range event {
		subscribe[index] = *NewSubscribe(domain, e, dependence, maxRetryTimes)
	}
	return subscribe
}

func (c *Config) AddSubscribe(subscribe []Subscribe) {
	subscribeMap := buildSubscribeMap(subscribe)
	for k, v := range subscribeMap {
		if eventMap, ok := c.subscribeMap[k]; ok {
			for e, s := range v {
				eventMap[e] = s
			}
			continue
		}
		c.subscribeMap[k] = v
	}
}

func (c Config) GetSubscribeList() []Subscribe {
	var subscribes []Subscribe
	for _, eventMap := range c.subscribeMap {
		for _, subscribe := range eventMap {
			subscribes = append(subscribes, subscribe)
		}
	}
	return subscribes
}

func (c Config) GetName() string {
	return c.name
}

func (c Config) GetSubscribe(domain string, event string) *Subscribe {
	if eventMap, ok := c.subscribeMap[domain]; ok {
		if s, okk := eventMap[event]; okk {
			return &s
		}
	}
	return nil
}

type Result struct {
	Success       bool   `json:"success"`
	Message       string `json:"message"`
	executor      int
	subscribeName string
}

func NewResult(success bool, message string) *Result {
	return &Result{
		Success:  success,
		Message:  message,
		executor: 0,
	}
}

type Request struct {
	eventId     string
	publishTime uint32

	domain       string
	entityId     string
	eventCode    string
	ParamsParser paramsParser
}

func NewRequest(eventId string, publishTime uint32, domain string, entityId string, eventCode string, eventContent map[string]interface{}) *Request {
	return &Request{
		eventId:      eventId,
		publishTime:  publishTime,
		domain:       domain,
		entityId:     entityId,
		eventCode:    eventCode,
		ParamsParser: *newParamsParser(eventContent),
	}
}

func (r *Request) GetEventId() string {
	return r.eventId
}

func (r *Request) GetPublishTime() uint32 {
	return r.publishTime
}

func (r *Request) GetDomain() string {
	return r.domain
}

func (r *Request) GetEntityId() string {
	return r.entityId
}

func (r *Request) GetEventCode() string {
	return r.eventCode
}

type paramsParser struct {
	params map[string]interface{}
}

func newParamsParser(params map[string]interface{}) *paramsParser {
	return &paramsParser{
		params: params,
	}
}

func (p *paramsParser) GetString(key string) string {
	return util.MapUtil.GetString(p.params, key)
}

func (p *paramsParser) GetInt64(key string, defaultValue int64) int64 {
	return util.MapUtil.GetInt64(p.params, key, defaultValue)
}

func (p *paramsParser) GetUint64(key string, defaultValue uint64) uint64 {
	return util.MapUtil.GetUint64(p.params, key, defaultValue)
}

func (p *paramsParser) GetFloat64(key string, defaultValue float64) float64 {
	return util.MapUtil.GetFloat64(p.params, key, defaultValue)
}

func (p *paramsParser) GetBool(key string, defaultValue bool) bool {
	return util.MapUtil.GetBool(p.params, key, defaultValue)
}

func (p *paramsParser) GetValue(key string) interface{} {
	return util.MapUtil.GetValue(p.params, key)
}

func (p *paramsParser) GetParams() map[string]interface{} {
	m := make(map[string]interface{}, len(p.params))
	for k, v := range p.params {
		m[k] = v
	}
	return m
}

type Subscriber interface {
	GetConfig() Config
	Execute(ctx context.Context, request Request) Result
}
