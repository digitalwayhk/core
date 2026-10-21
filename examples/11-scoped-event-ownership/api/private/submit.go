// 本文件演示Broker接收请求；它不冒充带业务事务的Outbox发布。
package private

import (
	"context"
	"encoding/json"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/business"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"time"
)

// Submit 使用认证用户A/B作为演示归属键与原始顺序键。
type Submit struct {
	Subject int `json:"subject"`
	runtime *business.Runtime
}

// NewSubmit 注入实例服务上下文。
func NewSubmit(r *business.Runtime) *Submit { return &Submit{runtime: r} }

// New 保留依赖而不保留上次请求。
func (s *Submit) New(interface{}) types.IRouter { return NewSubmit(s.runtime) }

// Parse 只绑定主题序号，身份不读请求正文。
func (s *Submit) Parse(req types.IRequest) error { return req.Bind(s) }

// Validation 仅支持两演示主题和用户。
func (s *Submit) Validation(req types.IRequest) error {
	uid, _ := req.GetUser()
	if (uid != "A" && uid != "B") || s.Subject < 0 || s.Subject > 1 {
		return types.NewPublicError(types.ErrorKindValidation, 0, "需要演示用户A/B和主题0/1", nil)
	}
	return nil
}

// Do 返回Broker接受事实；业务持久化由owner的批次事务另行完成。
func (s *Submit) Do(req types.IRequest) (interface{}, error) {
	uid, _ := req.GetUser()
	body, _ := json.Marshal(map[string]string{"key": uid})
	env := event.NewEnvelope(contract.ServiceName, "input", body)
	env.Subject = contract.Manifest().Subjects[s.Subject]
	env.ShardKey = "user:" + uid
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := s.runtime.Context.EventBridge.Publish(ctx, env.Subject, env); err != nil {
		return nil, err
	}
	return map[string]string{"eventID": env.ID, "state": "broker-accepted"}, nil
}

// GetResponse 声明接受响应，不能把此响应视为SQL已完成。
func (*Submit) GetResponse() interface{} {
	return map[string]string{"eventID": "", "state": "broker-accepted"}
}

// RouterInfo 注册标准Private路径。
func (s *Submit) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(s, router.WithServiceName(contract.ServiceName), router.WithPath("/api/"+contract.ServiceName+"/submit"))
}
