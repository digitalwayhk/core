package subscribe

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/dec/util"
	"github.com/zeromicro/go-zero/core/logx"
	"math"
	"strconv"
	"strings"
)

var noSubscriberMsg = "no subscriber"
var processedResult = innerResult(true, "processed")
var blockSubscriberResult = innerResult(false, "block")

var subscribeExecutor = 1
var innerExecutor = 2

type RequestWrapper struct {
	Request Request

	subscribeList   []string
	subscribeResult []Result
	retryTimes      uint32
}

func NewRequestWrapper(eventId string, publishTime uint32, domain string, entityId string, eventCode string, eventContent map[string]interface{}, subscribeList []string, subscribeResult []Result, retryTimes uint32) *RequestWrapper {
	return &RequestWrapper{
		Request:         *NewRequest(eventId, publishTime, domain, entityId, eventCode, eventContent),
		subscribeList:   subscribeList,
		subscribeResult: subscribeResult,
		retryTimes:      retryTimes,
	}
}

type SubscriberWrapper struct {
	subscriber Subscriber
}

func NewSubscriberWrapper(subscriber Subscriber) *SubscriberWrapper {
	return &SubscriberWrapper{
		subscriber: subscriber,
	}
}

func (s *SubscriberWrapper) Execute(ctx context.Context, requestWrapper RequestWrapper) {
	//获取订阅配置
	config := s.subscriber.GetConfig()
	subscribe := config.GetSubscribe(requestWrapper.Request.domain, requestWrapper.Request.eventCode)
	if subscribe == nil {
		return
	}
	//判断是否在订阅列表，并获取index
	subscribeList := requestWrapper.subscribeList
	index := -1
	for i, name := range subscribeList {
		if name == config.GetName() {
			index = i
			break
		}
	}
	if index == -1 {
		return
	}
	subscribeResult := requestWrapper.subscribeResult
	//过滤已处理成功的
	if subscribeResult[index].Success {
		subscribeResult[index] = *processedResult
		return
	}
	//依赖阻塞
	if len(subscribe.Dependence) > 0 {
		subscribeResultMap := buildDependenceResultMap(subscribe.Dependence, subscribeList, subscribeResult)
		for _, name := range subscribe.Dependence {
			if r, ok := subscribeResultMap[name]; ok && !r {
				subscribeResult[index] = *blockSubscriberResult
				return
			}
		}
	}
	//首次执行
	if requestWrapper.retryTimes == 0 {
		s.wrapperExecute(ctx, requestWrapper, index, config.GetName())
		return
	}
	//重试执行
	s.wrapperExecute(ctx, requestWrapper, index, config.GetName())
}

func buildDependenceResultMap(dependence []string, subscribeList []string, subscribeResult []Result) map[string]bool {
	//只构建依赖的result map，避免并发读写subscribeResult
	dependenceMap := make(map[string]bool)
	for _, item := range dependence {
		dependenceMap[item] = true
	}
	subscribeResultMap := make(map[string]bool)
	for i, name := range subscribeList {
		if dependenceMap[name] {
			subscribeResultMap[name] = subscribeResult[i].Success
		}
	}
	return subscribeResultMap
}

var ResultParser = &resultParser{}

type resultParser struct {
}

func (resultParser) BuildInitResult(size int) int {
	return pow(2, size) - 1
}

func (resultParser) BuildFailResult(result []Result) (int, string) {
	failResult := 0
	var failDetail []string
	for i := 0; i < len(result); i++ {
		if !result[i].Success {
			failResult += pow(2, i)
			msg := result[i].Message
			if result[i].executor == 0 {
				msg = noSubscriberMsg
			}
			failDetail = append(failDetail, strconv.Itoa(i)+":"+msg)
		}
	}
	return failResult, strings.Join(failDetail, ",")
}

func (resultParser) BuildExecuteFailResult(result []Result) string {
	var failDetail []string
	for i := 0; i < len(result); i++ {
		if !result[i].Success && result[i].executor == subscribeExecutor {
			failDetail = append(failDetail, result[i].subscribeName+":"+result[i].Message)
		}
	}
	return strings.Join(failDetail, ",")
}

func (resultParser) BuildExecuteSuccResult(result []Result) int {
	succResult := 0
	for i := 0; i < len(result); i++ {
		if result[i].Success && result[i].executor == subscribeExecutor {
			succResult += pow(2, i)
		}
	}
	return succResult
}

func (s *SubscriberWrapper) wrapperExecute(ctx context.Context, requestWrapper RequestWrapper, index int, name string) {
	// 从panic中恢复
	defer func() {
		if e := recover(); e != nil {
			logx.Errorf("[PANIC]wrapperExecute err,%v\n%s\n", e, util.RuntimeUtil.GetStack())
			requestWrapper.subscribeResult[index] = panicResult(e, subscribeExecutor, name)
		}
	}()
	//执行
	result := s.subscriber.Execute(ctx, requestWrapper.Request)
	result.executor = subscribeExecutor
	result.subscribeName = name
	requestWrapper.subscribeResult[index] = result
}

func pow(x, y int) int {
	return int(math.Pow(float64(x), float64(y)))
}

func innerResult(success bool, message string) *Result {
	return &Result{
		Success:  success,
		Message:  message,
		executor: innerExecutor,
	}
}

func panicResult(e interface{}, executor int, subscribeName string) Result {
	return Result{
		Success:       false,
		Message:       fmt.Sprintf("[PANIC]%v", e),
		executor:      executor,
		subscribeName: subscribeName,
	}
}
